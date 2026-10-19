package usage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	usagesvc "github.com/aoagents/agent-orchestrator/backend/internal/service/usage"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

// TestIngestorAppliesOpencodeV2SnapshotOnFinalization reads a real OpenCode 2
// store shape: only the bound session's assistant messages become events.
func TestIngestorAppliesOpencodeV2SnapshotOnFinalization(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1700000000, 0).UTC()
	store, session := seedUsageTestSession(t, t.TempDir(), "usage", domain.HarnessOpenCodeV2, domain.ActivityExited, "ses_opencode", now)
	binding := seedUsageTestBinding(t, store, session, "ses_opencode", domain.UsageBindingFinalizing, now)

	storePath := filepath.Join(t.TempDir(), "opencode.db")
	opencode := createOpencodeStore(t, storePath)
	insertOpencodeMessage(t, opencode, "msg_1", "ses_opencode", "assistant", 1,
		opencodeMessageJSON("deepseek-v4.1-flash", 1000, 200, 50, 20, 5))
	insertOpencodeMessage(t, opencode, "msg_2", "ses_opencode", "assistant", 2,
		opencodeMessageJSON("deepseek-v4.1-flash", 10, 0, 0, 3, 1))
	insertOpencodeMessage(t, opencode, "msg_3", "ses_opencode", "user", 3, `{"time":{"created":1}}`)
	insertOpencodeMessage(t, opencode, "msg_4", "another_session", "assistant", 4,
		opencodeMessageJSON("other-model", 7, 7, 7, 7, 7))

	source := seedOpencodeV2Source(t, store, binding, storePath, now)
	ingestor := NewIngestor(store, IngestorConfig{Clock: func() time.Time { return now }})
	if _, err := ingestor.Ingest(ctx, source.ID); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	runs, err := store.ListSessionUsageRuns(ctx, session.ID)
	if err != nil {
		t.Fatalf("list usage runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %+v, want one model", runs)
	}
	run := runs[0]
	if run.ModelID != "deepseek-v4.1-flash" || run.EventCount != 2 ||
		run.InputTokens != 1260 || run.CachedInputTokens != 200 || run.OutputTokens != 29 {
		t.Fatalf("run = %+v, want input 1260 cached 200 output 29 over 2 events", run)
	}
	if run.KnownInputCount != 2 || run.KnownOutputCount != 2 || run.BindingState != domain.UsageBindingComplete {
		t.Fatalf("run completeness = %+v", run)
	}

	sources, err := store.ListUsageSourcesForBinding(ctx, binding.ID)
	if err != nil || len(sources) != 1 || sources[0].State != domain.UsageSourceComplete {
		t.Fatalf("sources = %+v err = %v, want one complete source", sources, err)
	}

	// Reading the same store again is a replay, not new usage.
	if _, err := ingestor.Ingest(ctx, source.ID); err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	again, err := store.ListSessionUsageRuns(ctx, session.ID)
	if err != nil || len(again) != 1 || again[0].EventCount != 2 || again[0].InputTokens != 1260 {
		t.Fatalf("replayed runs = %+v err = %v", again, err)
	}
}

// TestIngestorWaitsForOpencodeV2Finalization keeps a live session's store out
// of the pipeline: OpenCode writes it continuously.
func TestIngestorWaitsForOpencodeV2Finalization(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1700000000, 0).UTC()
	store, session := seedUsageTestSession(t, t.TempDir(), "usage", domain.HarnessOpenCodeV2, domain.ActivityIdle, "ses_opencode", now)
	binding := seedUsageTestBinding(t, store, session, "ses_opencode", domain.UsageBindingActive, now)

	storePath := filepath.Join(t.TempDir(), "opencode.db")
	opencode := createOpencodeStore(t, storePath)
	insertOpencodeMessage(t, opencode, "msg_1", "ses_opencode", "assistant", 1,
		opencodeMessageJSON("deepseek-v4.1-flash", 1000, 0, 0, 20, 0))

	source := seedOpencodeV2Source(t, store, binding, storePath, now)
	ingestor := NewIngestor(store, IngestorConfig{Clock: func() time.Time { return now }})
	if _, err := ingestor.Ingest(ctx, source.ID); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	runs, err := store.ListSessionUsageRuns(ctx, session.ID)
	if err != nil || len(runs) != 0 {
		t.Fatalf("runs = %+v err = %v, want none before finalization", runs, err)
	}
	sources, err := store.ListUsageSourcesForBinding(ctx, binding.ID)
	if err != nil || len(sources) != 1 || sources[0].State == domain.UsageSourceComplete {
		t.Fatalf("sources = %+v err = %v, want an unfinished source", sources, err)
	}
}

// TestReadOpencodeV2SnapshotSkipsUnusableMessages covers the store shapes AO
// cannot read: no usage, no model, malformed JSON, another session.
func TestReadOpencodeV2SnapshotSkipsUnusableMessages(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "opencode.db")
	opencode := createOpencodeStore(t, storePath)
	insertOpencodeMessage(t, opencode, "msg_1", "ses_1", "assistant", 1, `{"time":{"created":5}}`)
	insertOpencodeMessage(t, opencode, "msg_2", "ses_1", "assistant", 2,
		`{"model":{"id":"m"},"tokens":{"input":1,"output":1,"reasoning":0,"cache":{"read":0,"write":0}}}`)
	insertOpencodeMessage(t, opencode, "msg_3", "ses_1", "assistant", 3, `not json`)
	insertOpencodeMessage(t, opencode, "msg_4", "ses_1", "user", 4,
		opencodeMessageJSON("m", 1, 0, 0, 1, 0))
	insertOpencodeMessage(t, opencode, "msg_5", "ses_2", "assistant", 5,
		opencodeMessageJSON("m", 1, 0, 0, 1, 0))

	readings, err := readOpencodeV2Snapshot(context.Background(), storePath, "ses_1")
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if len(readings) != 1 || readings[0].MessageID != "msg_2" {
		t.Fatalf("readings = %+v, want only the message that carries usage", readings)
	}
	if !readings[0].CreatedAt.IsZero() {
		t.Fatalf("a message without a timestamp must stay zero, got %v", readings[0].CreatedAt)
	}
	if _, err := readOpencodeV2Snapshot(context.Background(), storePath, ""); err == nil {
		t.Fatal("an empty native session id must not read a whole store")
	}
	if _, err := readOpencodeV2Snapshot(context.Background(), filepath.Join(t.TempDir(), "missing.db"), "ses_1"); err == nil {
		t.Fatal("a missing store must be an error")
	}
}

// TestOpencodeV2EventsFoldCacheAndReasoning pins the mapping: OpenCode reports
// cache reads and writes apart from input and reasoning apart from output.
func TestOpencodeV2EventsFoldCacheAndReasoning(t *testing.T) {
	source := domain.UsageSourceContext{
		Source:       domain.UsageSourceRecord{Kind: domain.UsageSourceOpencodeV2, NativeSessionID: "ses_1"},
		NativeRootID: "ses_1",
	}
	events := opencodeV2Events(source, []snapshotReading{
		{MessageID: "msg_1", ModelID: "m", Input: 100, CacheRead: 40, CacheWrite: 10, Output: 7, Reasoning: 3},
		{MessageID: "msg_1", ModelID: "m", Input: 100, CacheRead: 40, CacheWrite: 10, Output: 7, Reasoning: 3},
		{MessageID: "msg_2", ModelID: "", Input: 1},
	})
	if len(events) != 1 {
		t.Fatalf("events = %+v, want one deduplicated event", events)
	}
	event := events[0]
	if event.ProviderID != domain.UsageProviderAnthropic || event.ModelID != "m" ||
		event.MeasurementKind != domain.UsageMeasurementNativeReported {
		t.Fatalf("event identity = %+v", event)
	}
	tokens := event.Tokens
	if tokens.InputTokens == nil || *tokens.InputTokens != 150 ||
		tokens.CachedInputTokens == nil || *tokens.CachedInputTokens != 40 ||
		tokens.UncachedInputTokens == nil || *tokens.UncachedInputTokens != 110 ||
		tokens.OutputTokens == nil || *tokens.OutputTokens != 10 {
		t.Fatalf("tokens = %+v", tokens)
	}
	if event.ProviderUsageJSON == "" {
		t.Fatal("the native reading must survive in the provider usage object")
	}
}

func seedOpencodeV2Source(
	t *testing.T,
	store *sqlite.Store,
	binding domain.UsageBindingRecord,
	storePath string,
	now time.Time,
) domain.UsageSourceRecord {
	t.Helper()
	identity, err := usagesvc.SourceIdentity(context.Background(), storePath)
	mustNoError(t, err)
	source, err := store.InsertUsageSource(context.Background(), domain.UsageSourceRecord{
		BindingID:       binding.ID,
		Kind:            domain.UsageSourceOpencodeV2,
		NativeSessionID: binding.NativeRootID,
		ArtifactPath:    storePath,
		FileIdentity:    identity,
		State:           domain.UsageSourceActive,
		UpdatedAt:       now,
	})
	mustNoError(t, err)
	return source
}

func createOpencodeStore(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	mustNoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE session_message (
		id         text PRIMARY KEY,
		session_id text NOT NULL,
		type       text NOT NULL,
		seq        integer NOT NULL,
		data       text NOT NULL
	)`)
	mustNoError(t, err)
	return db
}

func insertOpencodeMessage(t *testing.T, db *sql.DB, id, sessionID, kind string, seq int, data string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO session_message (id, session_id, type, seq, data) VALUES (?, ?, ?, ?, ?)`,
		id, sessionID, kind, seq, data,
	)
	mustNoError(t, err)
}

func opencodeMessageJSON(model string, input, cacheRead, cacheWrite, output, reasoning int64) string {
	return fmt.Sprintf(
		`{"time":{"created":1792335140974},"model":{"id":%q},"tokens":{"input":%d,"output":%d,"reasoning":%d,"cache":{"read":%d,"write":%d}}}`,
		model, input, output, reasoning, cacheRead, cacheWrite,
	)
}
