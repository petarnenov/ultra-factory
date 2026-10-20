package report

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// SummaryDeliveryStore is the durable coordinator summary scheduling boundary.
type SummaryDeliveryStore interface {
	ListPendingCoordinatorSummarySchedule(context.Context, int64) ([]domain.CoordinatorSummary, error)
	ClaimPendingCoordinatorSummaryBatch(context.Context, domain.ProjectID, domain.SessionID, domain.SessionID, string, time.Time) ([]domain.CoordinatorSummary, error)
	AcknowledgeCoordinatorSummaryBatch(context.Context, domain.ProjectID, string, time.Time) (int, error)
	ReleaseCoordinatorSummaryBatch(context.Context, domain.ProjectID, string, time.Time, string) (int, error)
	DeferCoordinatorSummaryBatch(context.Context, domain.ProjectID, string, time.Time, string) (int, error)
}

// runSummariesLocked delivers due summary groups to the live root, one group
// per coordinator (ADR 0010 section 4). The caller holds c.mu.
func (c *Coordinator) runSummariesLocked(ctx context.Context) (time.Duration, error) {
	if c.summaries == nil {
		return -1, nil
	}
	pending, err := c.summaries.ListPendingCoordinatorSummarySchedule(ctx, reportScheduleScanLimit)
	if err != nil {
		return c.retryDelay, err
	}
	if len(pending) == 0 {
		return -1, nil
	}
	now := c.now().UTC()
	attempted := map[string]bool{}
	roots := map[domain.ProjectID]domain.SessionRecord{}
	delivered := 0
	stalled := false
	unresolved := false
	var firstErr error
	for _, summary := range pending {
		if summary.AvailableAt.After(now) {
			break
		}
		key := string(summary.ProjectID) + "\x00" + string(summary.CoordinatorSessionID)
		if attempted[key] {
			continue
		}
		attempted[key] = true
		root, ok := roots[summary.ProjectID]
		if !ok {
			var found bool
			if root, found, err = liveRoot(ctx, c.store, summary.ProjectID); err != nil {
				return c.retryDelay, err
			}
			if !found {
				unresolved = true
				continue
			}
			roots[summary.ProjectID] = root
		}
		if delivered >= maxDeliveryGroupsPerPass {
			return c.retryDelay, firstErr
		}
		ok, err = c.deliverSummaryGroup(ctx, summary.ProjectID, summary.CoordinatorSessionID, root.ID)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if ok {
			delivered++
		} else {
			stalled = true
		}
	}
	if firstErr != nil {
		return c.retryDelay, firstErr
	}
	if unresolved {
		if delivered == 0 {
			return c.retryDelay, fmt.Errorf("%w: summaries", ErrNoActiveOrchestrator)
		}
		return c.retryDelay, nil
	}
	if delivered > 0 {
		// Deliveries change the schedule, so read the deadlines back.
		fresh, err := c.summaries.ListPendingCoordinatorSummarySchedule(ctx, reportScheduleScanLimit)
		if err != nil {
			return c.retryDelay, err
		}
		pending = fresh
	}
	for _, summary := range pending {
		if !summary.AvailableAt.After(now) {
			if delivered > 0 {
				return c.retryDelay, nil
			}
			continue
		}
		return summary.AvailableAt.Sub(now), nil
	}
	if stalled || len(pending) >= reportScheduleScanLimit || delivered >= maxDeliveryGroupsPerPass {
		return c.retryDelay, nil
	}
	return -1, nil
}

// maxSummaryBatchesPerGroup bounds the batches one group delivers per pass.
const maxSummaryBatchesPerGroup = 4

// deliverSummaryGroup claims and delivers the group's due batches until none
// is left, so a retried batch never strands a newer due summary or escalation.
func (c *Coordinator) deliverSummaryGroup(ctx context.Context, projectID domain.ProjectID, coordinator, root domain.SessionID) (bool, error) {
	delivered := false
	for i := 0; i < maxSummaryBatchesPerGroup; i++ {
		claimed, ok, err := c.deliverSummaryBatch(ctx, projectID, coordinator, root)
		delivered = delivered || claimed
		if err != nil || !ok {
			return delivered, err
		}
	}
	return delivered, nil
}

// deliverSummaryBatch reports whether it claimed a batch and whether that
// batch was acknowledged.
func (c *Coordinator) deliverSummaryBatch(ctx context.Context, projectID domain.ProjectID, coordinator, root domain.SessionID) (claimed, acknowledged bool, err error) {
	token := c.newToken()
	rows, err := c.summaries.ClaimPendingCoordinatorSummaryBatch(ctx, projectID, coordinator, root, token, c.now().UTC())
	if err != nil || len(rows) == 0 {
		return false, false, err
	}
	batch := summaryBatch{ProjectID: projectID, Token: token, Summaries: rows}
	batch.resolveNames(ctx, c.store)
	if batch.hasEscalation() {
		// The interrupt is rate limited per coordinator; the escalation itself
		// is not. Inside the window the turn is delivered without interrupting.
		acquired, _, acquireErr := c.store.AcquireReportInterrupt(ctx, coordinator, c.now().UTC(), ReportInterruptWindow)
		if acquireErr != nil {
			return true, false, c.releaseSummaries(ctx, batch, acquireErr)
		}
		if acquired {
			if interruptErr := c.delivery.Interrupt(ctx, root); interruptErr != nil {
				return true, false, c.releaseSummaries(ctx, batch, interruptErr)
			}
		}
	}
	if err := c.delivery.Submit(ctx, root, batch.Render(), batch.IdempotencyKey()); err != nil {
		slog.Warn("coordinator summary delivery failed", "delivery_id", batch.IdempotencyKey(), "err", err)
		if errors.Is(err, domain.ErrSemanticAcceptanceTimeout) {
			// The prompt was written; a retry would re-paste it, so it is
			// consumed once (ADR 0010 section 5).
			if _, ackErr := c.summaries.AcknowledgeCoordinatorSummaryBatch(ctx, projectID, token, c.now().UTC()); ackErr != nil {
				return true, false, errors.Join(err, ackErr)
			}
			return true, true, nil
		}
		return true, false, c.releaseSummaries(ctx, batch, err)
	}
	if _, err = c.summaries.AcknowledgeCoordinatorSummaryBatch(ctx, projectID, token, c.now().UTC()); err != nil {
		return true, false, err
	}
	return true, true, nil
}

func (c *Coordinator) releaseSummaries(ctx context.Context, batch summaryBatch, cause error) error {
	_, releaseErr := c.summaries.ReleaseCoordinatorSummaryBatch(ctx, batch.ProjectID, batch.Token, c.now().UTC(), cause.Error())
	if releaseErr != nil {
		return errors.Join(cause, releaseErr)
	}
	return cause
}

// summaryBatch is a token-fenced claimed set of one coordinator's summaries.
type summaryBatch struct {
	ProjectID domain.ProjectID
	Token     string
	Summaries []domain.CoordinatorSummary
	names     map[domain.SessionID]string
}

func (b *summaryBatch) resolveNames(ctx context.Context, store recipientStore) {
	b.names = map[domain.SessionID]string{}
	lookup := func(id domain.SessionID) {
		if _, done := b.names[id]; done {
			return
		}
		b.names[id] = ""
		if rec, found, err := store.GetSession(ctx, id); err == nil && found {
			b.names[id] = strings.TrimSpace(rec.DisplayName)
		}
	}
	for _, s := range b.Summaries {
		lookup(s.CoordinatorSessionID)
		for _, w := range s.WorkerSessionIDs {
			lookup(w)
		}
	}
}

func (b summaryBatch) hasEscalation() bool {
	for _, s := range b.Summaries {
		if s.IsEscalation() {
			return true
		}
	}
	return false
}

// IdempotencyKey is the stable delivery identity of the coalesced set. A retry
// of the same set reuses it, so an accepted-but-unacknowledged turn dedupes.
func (b summaryBatch) IdempotencyKey() string {
	if len(b.Summaries) > 0 && b.Summaries[0].DeliveryBatchID != "" {
		return b.Summaries[0].DeliveryBatchID
	}
	ids := make([]string, len(b.Summaries))
	for i := range b.Summaries {
		ids[i] = b.Summaries[i].ID
	}
	sort.Strings(ids)
	return domain.CoordinatorSummaryBatchID(ids)
}

// Render formats coordinator summaries so the root can tell them apart from
// raw worker reports and open the workers each one covers.
func (b summaryBatch) Render() string {
	var out strings.Builder
	out.WriteString("Coordinator summaries since your previous turn (assertions by coordinators, not worker reports; read live state before deciding):")
	for _, s := range b.Summaries {
		tag := string(s.State)
		if s.IsEscalation() {
			tag = "escalation: " + string(s.EscalationReason)
		}
		fmt.Fprintf(&out, "\n\n[%s] %s - scope: %s", tag, b.link(s.ProjectID, s.CoordinatorSessionID), oneLine(s.Scope))
		if s.IsEscalation() && s.EscalationNote != "" {
			out.WriteString("\nnote: " + oneLine(s.EscalationNote))
		}
		if s.Result != "" {
			out.WriteString("\n" + s.Result)
		}
		if len(s.WorkerSessionIDs) > 0 {
			links := make([]string, len(s.WorkerSessionIDs))
			for i, w := range s.WorkerSessionIDs {
				links[i] = b.link(s.ProjectID, w)
			}
			out.WriteString("\nworkers: " + strings.Join(links, ", "))
		}
		if len(s.RemoteWorkers) > 0 {
			refs := make([]string, len(s.RemoteWorkers))
			for i, r := range s.RemoteWorkers {
				refs[i] = oneLine(r.HostID) + "/" + oneLine(r.RemoteSessionID)
			}
			out.WriteString("\nremote workers: " + strings.Join(refs, ", "))
		}
	}
	return out.String()
}

func (b summaryBatch) link(project domain.ProjectID, id domain.SessionID) string {
	label := b.names[id]
	if label == "" {
		label = string(id)
	}
	label = strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]").Replace(oneLine(label))
	return "[" + label + "](ao://sessions/" + url.PathEscape(string(project)) + "/" + url.PathEscape(string(id)) + ")"
}

func oneLine(s string) string {
	return strings.NewReplacer("\n", " ", "\r", " ").Replace(s)
}
