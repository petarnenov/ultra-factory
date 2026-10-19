package usage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/pricing"
)

// snapshotSourceKind reports whether a source is read once from a native store
// that has no append-only transcript.
func snapshotSourceKind(kind domain.UsageSourceKind) bool {
	return kind == domain.UsageSourceOpencodeV2
}

// maxSnapshotMessages bounds one snapshot read so a pathological native store
// cannot turn a single ingestion into an unbounded chunk.
const maxSnapshotMessages = 20000

// snapshotReader reads every native usage reading one snapshot source holds for
// one native session. It is injectable so ingestion tests need no real store.
type snapshotReader func(ctx context.Context, storePath, nativeSessionID string) ([]snapshotReading, error)

// snapshotReading is one assistant message's native usage counters.
type snapshotReading struct {
	MessageID string
	ModelID   string
	Input     int64
	CacheRead int64
	// CacheWrite is the cache-creation part of input.
	CacheWrite int64
	Output     int64
	// Reasoning is reported apart from Output and is billed as output.
	Reasoning int64
	CreatedAt time.Time
}

// ingestSnapshot applies a source whose native store has no append-only
// transcript: OpenCode 2 keeps per-message usage in SQLite, read once here.
func (i *Ingestor) ingestSnapshot(ctx context.Context, source domain.UsageSourceContext, now time.Time) (IngestResult, error) {
	result := IngestResult{}
	if source.Source.State == domain.UsageSourceComplete {
		return result, nil
	}
	// A snapshot has no partial reading worth keeping: until the session ends
	// the native store is still being written, so the source waits.
	if source.BindingState != domain.UsageBindingFinalizing {
		return result, nil
	}
	readings, err := i.readSnapshot(ctx, source.Source.ArtifactPath, source.Source.NativeSessionID)
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return i.retrySource(ctx, source.Source, domain.UsageErrorSourceReadFailed, now, err)
	}
	cursor := domain.SourceCursorState{
		ByteOffset: source.Source.ByteOffset,
		State:      domain.UsageSourceComplete,
		UpdatedAt:  now,
	}
	if err := i.applyEvents(ctx, source, cursor, opencodeV2Events(source, readings)); err != nil {
		if errors.Is(err, domain.ErrUsageSourceEventConflict) {
			return result, i.settleConflict(ctx, source, now, err)
		}
		return result, fmt.Errorf("apply usage source %d: %w", source.Source.ID, err)
	}
	return result, i.completeBinding(ctx, source.Source.BindingID, now)
}

// settleConflict retires a snapshot source whose stored events disagree with a
// re-read of the native store, so the disagreement is visible once.
func (i *Ingestor) settleConflict(ctx context.Context, source domain.UsageSourceContext, now time.Time, cause error) error {
	if _, markErr := i.store.MarkUsageSourceState(
		ctx,
		source.Source.ID,
		domain.UsageSourceComplete,
		domain.UsageErrorSourceEventConflict,
		nil,
		now,
	); markErr != nil {
		return errors.Join(cause, markErr)
	}
	if source.BindingState == domain.UsageBindingFinalizing {
		return i.completeBinding(ctx, source.Source.BindingID, now)
	}
	return nil
}

// applyEvents prices a chunk against one fenced pricing snapshot and writes it
// with its cursor atomically. An unnamed provider stays for the repairer.
func (i *Ingestor) applyEvents(
	ctx context.Context,
	source domain.UsageSourceContext,
	cursor domain.SourceCursorState,
	events []domain.ModelUsageEvent,
) error {
	apply := func() error {
		return i.store.ApplyUsageChunk(
			ctx,
			source.Source.ID,
			source.Source.ByteOffset,
			source.Source.UpdatedAt,
			cursor,
			events,
		)
	}
	if i.pricing == nil {
		return apply()
	}
	return i.pricing.WithSnapshot(ctx, func(snapshot *pricing.Snapshot) error {
		for index := range events {
			estimate, estimateErr := snapshot.Estimate(events[index])
			if estimateErr != nil {
				events[index].Costs.PricingVersion = snapshot.ProviderVersion(events[index].BillingProviderID)
				i.onPricingError(fmt.Errorf("estimate usage event %q: %w", events[index].SourceEventKey, estimateErr))
				continue
			}
			events[index].Costs = domain.UsageEventCosts{
				InputCostNanos:       estimate.InputNanos,
				CachedInputCostNanos: estimate.CachedInputNanos,
				OutputCostNanos:      estimate.OutputNanos,
				EstimatedCostNanos:   estimate.TotalNanos,
				PricingVersion:       estimate.PricingVersion,
			}
		}
		return apply()
	})
}

// opencodeV2Events normalizes OpenCode 2's per-message counters into AO's
// vector: input covers cached plus uncached, output covers reasoning.
func opencodeV2Events(source domain.UsageSourceContext, readings []snapshotReading) []domain.ModelUsageEvent {
	events := make([]domain.ModelUsageEvent, 0, len(readings))
	seen := make(map[string]struct{}, len(readings))
	for _, reading := range readings {
		if reading.MessageID == "" || reading.ModelID == "" {
			continue
		}
		tokens, ok := normalizeAnthropicUsage(
			reading.Input,
			reading.CacheWrite,
			reading.CacheRead,
			reading.Output+reading.Reasoning,
			nil,
			nil,
		)
		if !ok {
			continue
		}
		event := domain.ModelUsageEvent{
			ProviderID:        domain.UsageProviderAnthropic,
			ModelID:           reading.ModelID,
			MeasurementKind:   domain.UsageMeasurementNativeReported,
			Tokens:            tokens,
			ProviderUsageJSON: opencodeProviderUsage(reading),
			CreatedAt:         reading.CreatedAt,
			SourceEventKey: stableSourceEventKey(
				"opencode-v2",
				source.NativeRootID,
				source.Source.NativeSessionID,
				reading.MessageID,
			),
		}
		if _, duplicate := seen[event.SourceEventKey]; duplicate {
			continue
		}
		seen[event.SourceEventKey] = struct{}{}
		events = append(events, event)
	}
	return events
}

// opencodeUsageObject is OpenCode 2's own token reading, stored verbatim.
type opencodeUsageObject struct {
	Input     int64 `json:"input"`
	Output    int64 `json:"output"`
	Reasoning int64 `json:"reasoning"`
	Cache     struct {
		Read  int64 `json:"read"`
		Write int64 `json:"write"`
	} `json:"cache"`
}

func opencodeProviderUsage(reading snapshotReading) string {
	var object opencodeUsageObject
	object.Input, object.Output, object.Reasoning = reading.Input, reading.Output, reading.Reasoning
	object.Cache.Read, object.Cache.Write = reading.CacheRead, reading.CacheWrite
	encoded, err := json.Marshal(object)
	if err != nil {
		return ""
	}
	return boundedProviderUsage(encoded)
}

// readOpencodeV2Snapshot reads one session's assistant messages from OpenCode
// 2's SQLite store. The store is opened read-only.
func readOpencodeV2Snapshot(ctx context.Context, storePath, nativeSessionID string) ([]snapshotReading, error) {
	if nativeSessionID == "" {
		return nil, errors.New("opencode-v2 snapshot: empty native session id")
	}
	storeURL := url.URL{Path: storePath}
	db, err := sql.Open("sqlite", "file:"+storeURL.EscapedPath()+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, data
		FROM session_message
		WHERE session_id = ? AND type = 'assistant'
		ORDER BY seq
		LIMIT ?`, nativeSessionID, maxSnapshotMessages)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var readings []snapshotReading
	for rows.Next() {
		var id, data string
		if err := rows.Scan(&id, &data); err != nil {
			return nil, err
		}
		reading, ok := decodeOpencodeMessage(id, data)
		if !ok {
			continue
		}
		readings = append(readings, reading)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return readings, nil
}

// opencodeStoredMessage is the part of OpenCode 2's assistant message AO reads.
type opencodeStoredMessage struct {
	Time struct {
		Created int64 `json:"created"`
	} `json:"time"`
	Model struct {
		ID string `json:"id"`
	} `json:"model"`
	Tokens *opencodeUsageObject `json:"tokens"`
}

func decodeOpencodeMessage(id, data string) (snapshotReading, bool) {
	var message opencodeStoredMessage
	if err := json.Unmarshal([]byte(data), &message); err != nil {
		return snapshotReading{}, false
	}
	if message.Tokens == nil || message.Model.ID == "" {
		return snapshotReading{}, false
	}
	reading := snapshotReading{
		MessageID:  id,
		ModelID:    message.Model.ID,
		Input:      message.Tokens.Input,
		CacheRead:  message.Tokens.Cache.Read,
		CacheWrite: message.Tokens.Cache.Write,
		Output:     message.Tokens.Output,
		Reasoning:  message.Tokens.Reasoning,
	}
	if message.Time.Created > 0 {
		reading.CreatedAt = time.UnixMilli(message.Time.Created).UTC()
	}
	return reading, true
}
