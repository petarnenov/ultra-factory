package report

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	coordownership "github.com/aoagents/agent-orchestrator/backend/internal/service/ownership"
)

// ReportInterruptWindow is the durable per-worker urgent interruption limit.
const ReportInterruptWindow = 3 * time.Minute

// ErrReportDeliveryModeUnsupported reports a target without semantic delivery.
var ErrReportDeliveryModeUnsupported = errors.New("report delivery requires a semantic conversation")

// DeliveryStore is the durable report scheduling and routing boundary.
type DeliveryStore interface {
	ListSessions(context.Context, domain.ProjectID) ([]domain.SessionRecord, error)
	GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
	ListPendingReportSchedule(context.Context, int64) ([]domain.ReportRecord, error)
	ClaimPendingReportBatch(context.Context, domain.ProjectID, domain.SessionID, domain.SessionID, string, time.Time, ...domain.SessionID) ([]domain.ReportRecord, error)
	AcknowledgeReportBatch(context.Context, domain.ProjectID, string, time.Time) (int, error)
	ReleaseReportBatch(context.Context, domain.ProjectID, string, time.Time, string) (int, error)
	DeferReportBatch(context.Context, domain.ProjectID, string, time.Time, string) (int, error)
	AcquireReportInterrupt(context.Context, domain.SessionID, time.Time, time.Duration) (bool, time.Time, error)
	ClearSessionOwnerIf(context.Context, domain.SessionID, domain.SessionID, domain.OwnershipEventReason, time.Time) (domain.SessionRecord, bool, error)
	GetRemoteWorkerLink(context.Context, string, domain.SessionID) (domain.RemoteWorkerLink, bool, error)
	ClearRemoteWorkerOwnerIf(context.Context, string, domain.SessionID, domain.SessionID, domain.OwnershipEventReason, time.Time) (domain.RemoteWorkerLink, bool, error)
}

// Delivery submits accepted semantic context and interrupts active work.
type Delivery interface {
	Submit(context.Context, domain.SessionID, string, string) error
	Interrupt(context.Context, domain.SessionID) error
}

// Coordinator delivers durable report batches to each worker's owning
// coordinator, or to the project's live root when the worker is root-owned.
type Coordinator struct {
	store      DeliveryStore
	delivery   Delivery
	now        func() time.Time
	newToken   func() string
	retryDelay time.Duration
	wake       chan struct{}
	mu         sync.Mutex
	summaries  SummaryDeliveryStore
	// escalated holds exited coordinators the root was already told about.
	escalated map[domain.SessionID]bool
}

// CoordinatorDeps configures durable scheduling and delivery.
type CoordinatorDeps struct {
	Store      DeliveryStore
	Delivery   Delivery
	Now        func() time.Time
	NewToken   func() string
	RetryDelay time.Duration
	// Summaries enables coordinator summary delivery to the root. Optional.
	Summaries SummaryDeliveryStore
}

// NewCoordinator constructs an idle report delivery coordinator.
func NewCoordinator(d CoordinatorDeps) *Coordinator {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.NewToken == nil {
		d.NewToken = uuid.NewString
	}
	if d.RetryDelay <= 0 {
		d.RetryDelay = 5 * time.Second
	}
	return &Coordinator{store: d.Store, delivery: d.Delivery, now: d.Now, newToken: d.NewToken, retryDelay: d.RetryDelay, wake: make(chan struct{}, 1), summaries: d.Summaries, escalated: map[domain.SessionID]bool{}}
}

// Wake asks the coordinator to inspect durable scheduling state.
func (c *Coordinator) Wake() {
	if c == nil {
		return
	}
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// Start runs delivery until ctx is cancelled and returns its completion signal.
func (c *Coordinator) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() { defer close(done); c.run(ctx) }()
	return done
}

func (c *Coordinator) run(ctx context.Context) {
	for {
		delay, err := c.RunDue(ctx)
		if err != nil {
			delay = c.retryDelay
		}
		if delay < 0 {
			delay = time.Hour
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-c.wake:
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
		}
	}
}

// reportScheduleScanLimit bounds how many pending reports one scheduling pass
// inspects while it groups them by recipient.
const reportScheduleScanLimit = 256

// maxDeliveryGroupsPerPass bounds semantic turns per pass so one busy project
// cannot starve the rest of the daemon.
const maxDeliveryGroupsPerPass = 32

// RunDue performs bounded due delivery and returns the delay until the next
// durable deadline. Tests call it directly with fake time.
func (c *Coordinator) RunDue(ctx context.Context) (time.Duration, error) {
	if c == nil || c.store == nil || c.delivery == nil {
		return -1, errors.New("report coordinator dependencies are required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	reportDelay, reportErr := c.runReportsLocked(ctx)
	summaryDelay, summaryErr := c.runSummariesLocked(ctx)
	if err := errors.Join(reportErr, summaryErr); err != nil {
		return c.retryDelay, err
	}
	return earliestDelay(reportDelay, summaryDelay), nil
}

// earliestDelay merges two scheduler deadlines; a negative delay means idle.
func earliestDelay(a, b time.Duration) time.Duration {
	switch {
	case a < 0:
		return b
	case b < 0:
		return a
	case b < a:
		return b
	}
	return a
}

// runReportsLocked is the worker report pass; the caller holds c.mu.
func (c *Coordinator) runReportsLocked(ctx context.Context) (time.Duration, error) {
	pending, err := c.store.ListPendingReportSchedule(ctx, reportScheduleScanLimit)
	if err != nil {
		return c.retryDelay, err
	}
	if len(pending) == 0 {
		return -1, nil
	}
	now := c.now().UTC()
	attempted := map[string]bool{}
	delivered := 0
	stalled := false
	stalledClaim := false
	var firstErr error
	var unresolved domain.ProjectID
	for _, report := range pending {
		due := !report.AvailableAt.After(now)
		if !due {
			if !c.terminatedWorker(ctx, report.SessionID) {
				break
			}
			// A terminated worker cannot report again, so its queued report
			// is flushed now instead of trickling in after its deadline.
		}
		group, err := c.localGroup(ctx, report)
		if err != nil {
			return c.retryDelay, err
		}
		key := string(report.ProjectID) + "\x00" + string(group.Recipient) + "\x00" + string(group.ExitedOwner)
		if !group.Found {
			key = string(report.ProjectID) + "\x00"
		}
		if attempted[key] {
			continue
		}
		attempted[key] = true
		if !group.Found {
			if unresolved == "" {
				unresolved = report.ProjectID
			}
			continue
		}
		if delivered >= maxDeliveryGroupsPerPass {
			// More due groups remain; run again shortly instead of sleeping.
			return c.retryDelay, firstErr
		}
		claimed, acknowledged, err := c.deliverGroup(ctx, report.ProjectID, group)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if claimed {
			delivered++
		} else {
			stalled = true
		}
		if claimed && !acknowledged {
			stalledClaim = true
		}
	}
	if firstErr != nil {
		return c.retryDelay, firstErr
	}
	if unresolved != "" {
		if delivered == 0 {
			return c.retryDelay, fmt.Errorf("%w: %s", ErrNoActiveOrchestrator, unresolved)
		}
		// A helper group was delivered; retry the root-addressed group shortly
		// instead of letting a root outage park it behind the next deadline.
		return c.retryDelay, nil
	}
	schedule := pending
	if stalledClaim || delivered > 0 {
		// A claim that acknowledged nothing may have moved its batch later, and
		// a claim covers one batch only, so read the leftovers back.
		fresh, err := c.store.ListPendingReportSchedule(ctx, reportScheduleScanLimit)
		if err != nil {
			return c.retryDelay, nil //nolint:nilerr // retry; the pass itself succeeded
		}
		schedule = fresh
	}
	if delivered > 0 && anyDue(schedule, now) {
		// A due report outside the claimed batch would otherwise wait for the
		// next wake-up or the idle timer.
		return c.retryDelay, nil
	}
	if next, ok := nextPendingDelay(schedule, now); ok {
		return next, nil
	}
	if delivered > 0 {
		if delivered >= maxDeliveryGroupsPerPass || len(pending) >= reportScheduleScanLimit {
			return c.retryDelay, nil
		}
		return -1, nil
	}
	if stalled || len(pending) >= reportScheduleScanLimit {
		return c.retryDelay, nil
	}
	return -1, nil
}

// terminatedWorker reports a worker that will never produce another report.
func (c *Coordinator) terminatedWorker(ctx context.Context, id domain.SessionID) bool {
	session, found, err := c.store.GetSession(ctx, id)
	return err == nil && found && session.IsTerminated
}

// anyDue reports whether schedule holds a report already due at now.
func anyDue(schedule []domain.ReportRecord, now time.Time) bool {
	for _, report := range schedule {
		if !report.AvailableAt.After(now) {
			return true
		}
	}
	return false
}

// nextPendingDelay returns the earliest future deadline in schedule order.
func nextPendingDelay(pending []domain.ReportRecord, now time.Time) (time.Duration, bool) {
	for _, report := range pending {
		if report.AvailableAt.After(now) {
			return report.AvailableAt.Sub(now), true
		}
	}
	return 0, false
}

// recipientGroup is one resolved delivery group: its recipient and the
// project's live root, which anchors NULL owners in the claim predicate.
type recipientGroup struct {
	Recipient domain.SessionID
	Root      domain.SessionID
	// ExitedOwner is an exited coordinator whose workers the root takes over.
	ExitedOwner domain.SessionID
	Found       bool
}

// recipientStore is the ownership and root lookup a recipient resolution
// needs; the durable store and the test fake both satisfy it.
type recipientStore interface {
	ListSessions(context.Context, domain.ProjectID) ([]domain.SessionRecord, error)
	GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
}

// resolveOwnerRecipient applies ADR 0010 section 3: a live same-project
// coordinator owner wins; otherwise the project's newest live root is used.
// An owner whose agent exited is not a recipient but keeps its ownership
// (ownerExited), so the caller routes to root without repairing it.
func resolveOwnerRecipient(ctx context.Context, store recipientStore, projectID domain.ProjectID, owner *domain.SessionID) (ownerRec domain.SessionRecord, ownerValid, ownerExited bool, root domain.SessionRecord, rootFound bool, err error) {
	root, rootFound, err = liveRoot(ctx, store, projectID)
	if err != nil {
		return domain.SessionRecord{}, false, false, domain.SessionRecord{}, false, err
	}
	if owner == nil {
		return domain.SessionRecord{}, false, false, root, rootFound, nil
	}
	ownerRec, found, err := store.GetSession(ctx, *owner)
	if err != nil {
		return domain.SessionRecord{}, false, false, root, rootFound, err
	}
	if found && coordownership.IsLiveCoordinator(ownerRec, projectID) {
		if ownerRec.Activity.State == domain.ActivityExited {
			return ownerRec, false, true, root, rootFound, nil
		}
		return ownerRec, true, false, root, rootFound, nil
	}
	return domain.SessionRecord{}, false, false, root, rootFound, nil
}

// escalateExitedOwner tells the root once per exit that a coordinator needs
// recovery; a coordinator that is running again re-arms the notice.
func (c *Coordinator) escalateExitedOwner(ctx context.Context, owner domain.SessionRecord, root domain.SessionRecord, rootFound bool) {
	if !rootFound || c.delivery == nil || c.escalated[owner.ID] {
		return
	}
	text := fmt.Sprintf("Coordinator %s exited and needs recovery. Its workers' reports are routed to you until it is restored or terminated; ownership is unchanged.", owner.ID)
	if err := c.delivery.Submit(ctx, root.ID, text, "coordinator-exited:"+string(owner.ID)); err != nil {
		slog.Warn("coordinator recovery notice failed", "coordinator", owner.ID, "root", root.ID, "err", err)
		return
	}
	c.escalated[owner.ID] = true
}

// ownerRepairAttempts bounds the re-resolve loop after a lost compare-and-set.
const ownerRepairAttempts = 3

// localGroup resolves a local report's recipient from durable ownership and
// repairs a stale owner to root ownership before falling back.
func (c *Coordinator) localGroup(ctx context.Context, report domain.ReportRecord) (recipientGroup, error) {
	for attempt := 0; ; attempt++ {
		var owner *domain.SessionID
		if worker, found, err := c.store.GetSession(ctx, report.SessionID); err != nil {
			return recipientGroup{}, err
		} else if found {
			owner = worker.OwnerSessionID
		}
		ownerRec, ownerValid, ownerExited, root, rootFound, err := resolveOwnerRecipient(ctx, c.store, report.ProjectID, owner)
		if err != nil {
			return recipientGroup{}, err
		}
		group := recipientGroup{}
		if rootFound {
			group.Root = root.ID
		}
		if ownerValid {
			delete(c.escalated, ownerRec.ID)
			group.Recipient, group.Found = ownerRec.ID, true
			return group, nil
		}
		if ownerExited {
			c.escalateExitedOwner(ctx, ownerRec, root, rootFound)
			group.ExitedOwner = ownerRec.ID
		} else if owner != nil {
			_, cleared, err := c.store.ClearSessionOwnerIf(ctx, report.SessionID, *owner, domain.OwnershipOwnerNotLiveReconciled, c.now().UTC())
			if err != nil {
				return recipientGroup{}, err
			}
			// The owner moved after it was read: resolve the new one.
			if !cleared && attempt < ownerRepairAttempts {
				continue
			}
			if !cleared {
				return recipientGroup{}, fmt.Errorf("%w: owner of %s keeps changing", ErrRecipientUnavailable, report.SessionID)
			}
		}
		if rootFound {
			group.Recipient, group.Root, group.Found = root.ID, root.ID, true
		}
		return group, nil
	}
}

// deliverGroup reports whether it claimed a batch and whether it acknowledged
// one; only an acknowledged batch has finished its delivery.
func (c *Coordinator) deliverGroup(ctx context.Context, projectID domain.ProjectID, group recipientGroup) (claimed, acknowledged bool, err error) {
	batch, err := c.claim(ctx, projectID, group.Recipient, group.Root, group.ExitedOwner)
	if err != nil || len(batch.Reports) == 0 {
		return false, false, err
	}
	if batch.hasState(domain.ReportStuck) {
		interrupted := false
		var nextInterrupt time.Time
		for _, worker := range batch.stuckWorkers() {
			acquired, next, acquireErr := c.store.AcquireReportInterrupt(ctx, worker, c.now().UTC(), ReportInterruptWindow)
			if acquireErr != nil {
				return true, false, c.release(ctx, batch, acquireErr)
			}
			if acquired {
				interrupted = true
				if interruptErr := c.delivery.Interrupt(ctx, group.Recipient); interruptErr != nil {
					return true, false, c.release(ctx, batch, interruptErr)
				}
			} else if nextInterrupt.IsZero() || next.Before(nextInterrupt) {
				nextInterrupt = next
			}
		}
		if !interrupted && !batch.hasState(domain.ReportNeedsInput) {
			_, deferErr := c.store.DeferReportBatch(ctx, batch.ProjectID, batch.Token, nextInterrupt, "repeated stuck report coalesced inside interrupt window")
			return true, false, deferErr
		}
	}
	if err := c.delivery.Submit(ctx, group.Recipient, batch.Render(), batch.IdempotencyKey()); err != nil {
		if !errors.Is(err, domain.ErrSemanticAcceptanceTimeout) {
			slog.Warn("report delivery failed", "delivery_id", batch.IdempotencyKey(), "recipient", group.Recipient, "err", err)
			return true, false, c.release(ctx, batch, err)
		}
		// The prompt was written; only its acceptance hook is late. Re-arming
		// the batch re-pastes it on every retry, so it is consumed once here.
		slog.Warn("report delivery unconfirmed; batch consumed", "delivery_id", batch.IdempotencyKey(), "recipient", group.Recipient, "err", err)
		if ackErr := c.consume(ctx, batch); ackErr != nil {
			return true, false, errors.Join(err, ackErr)
		}
		return true, true, nil
	}
	if err := c.consume(ctx, batch); err != nil {
		return true, false, err
	}
	return true, true, nil
}

// consume acknowledges a submitted batch, so no later pass sends it again.
func (c *Coordinator) consume(ctx context.Context, batch PreparedBatch) error {
	_, err := c.store.AcknowledgeReportBatch(ctx, batch.ProjectID, batch.Token, c.now().UTC())
	return err
}

// ErrNoActiveOrchestrator reports a project with no orchestrator to deliver to.
var ErrNoActiveOrchestrator = errors.New("project has no active orchestrator")

// ErrRecipientUnavailable reports a resolved recipient that is no longer
// eligible; the report stays pending and the next pass re-resolves it.
var ErrRecipientUnavailable = errors.New("report recipient is unavailable")

// ResolveRemoteRecipient applies the ownership rule to a remote link, using
// the owner re-read under the delivery lock, and repairs a stale one.
func (c *Coordinator) ResolveRemoteRecipient(ctx context.Context, link domain.RemoteWorkerLink) (domain.SessionRecord, bool, error) {
	if c == nil || c.store == nil {
		return domain.SessionRecord{}, false, errors.New("report coordinator dependencies are required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for attempt := 0; ; attempt++ {
		current, found, err := c.store.GetRemoteWorkerLink(ctx, link.HostID, link.RemoteSessionID)
		if err != nil {
			return domain.SessionRecord{}, false, err
		}
		if !found {
			current = link
			current.OwnerSessionID = nil
		}
		owner, ownerValid, ownerExited, root, rootFound, err := resolveOwnerRecipient(ctx, c.store, current.ProjectID, current.OwnerSessionID)
		if err != nil {
			return domain.SessionRecord{}, false, err
		}
		if ownerValid {
			delete(c.escalated, owner.ID)
			return owner, true, nil
		}
		if ownerExited {
			c.escalateExitedOwner(ctx, owner, root, rootFound)
		} else if current.OwnerSessionID != nil {
			_, cleared, err := c.store.ClearRemoteWorkerOwnerIf(ctx, current.HostID, current.RemoteSessionID, *current.OwnerSessionID, domain.OwnershipOwnerNotLiveReconciled, c.now().UTC())
			if err != nil {
				return domain.SessionRecord{}, false, err
			}
			// The owner moved after it was read: resolve the new one.
			if !cleared && attempt < ownerRepairAttempts {
				continue
			}
			if !cleared {
				return domain.SessionRecord{}, false, fmt.Errorf("%w: owner of %s keeps changing", ErrRecipientUnavailable, current.RemoteSessionID)
			}
		}
		return root, rootFound, nil
	}
}

// DeliverExternal submits one resolved owner group pulled from a peer daemon
// in one turn. The caller records it as delivered only after this returns nil.
func (c *Coordinator) DeliverExternal(ctx context.Context, projectID domain.ProjectID, recipient domain.SessionID, reports []domain.ReportRecord) error {
	if c == nil || c.store == nil || c.delivery == nil {
		return errors.New("report coordinator dependencies are required")
	}
	if len(reports) == 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	rec, found, err := c.store.GetSession(ctx, recipient)
	if err != nil {
		return err
	}
	if !found || rec.IsTerminated || rec.ProjectID != projectID ||
		(rec.Kind != domain.KindOrchestrator && rec.Kind != domain.KindCoordinator) ||
		rec.Activity.State == domain.ActivityExited {
		return fmt.Errorf("%w: %s", ErrRecipientUnavailable, recipient)
	}
	batch := PreparedBatch{ProjectID: projectID, Reports: reports}
	return c.delivery.Submit(ctx, recipient, batch.Render(), batch.IdempotencyKey())
}

func (c *Coordinator) activeOrchestrator(ctx context.Context, projectID domain.ProjectID) (domain.SessionRecord, bool, error) {
	return liveRoot(ctx, c.store, projectID)
}

// liveRoot returns the newest live root orchestrator in a project. Exited
// roots are skipped, matching the report recipient selector in ADR 0010.
func liveRoot(ctx context.Context, store recipientStore, projectID domain.ProjectID) (domain.SessionRecord, bool, error) {
	recs, err := store.ListSessions(ctx, projectID)
	if err != nil {
		return domain.SessionRecord{}, false, err
	}
	var selected domain.SessionRecord
	for _, rec := range recs {
		if rec.Kind != domain.KindOrchestrator || rec.IsTerminated || rec.Activity.State == domain.ActivityExited {
			continue
		}
		if selected.ID == "" || rec.CreatedAt.After(selected.CreatedAt) {
			selected = rec
		}
	}
	return selected, selected.ID != "", nil
}

// PreparedBatch is a token-fenced durable batch awaiting acceptance.
type PreparedBatch struct {
	ProjectID domain.ProjectID
	Token     string
	ID        string
	Reports   []domain.ReportRecord
}

func (c *Coordinator) claim(ctx context.Context, projectID domain.ProjectID, recipient, root, exitedOwner domain.SessionID) (PreparedBatch, error) {
	token := c.newToken()
	reports, err := c.store.ClaimPendingReportBatch(ctx, projectID, recipient, root, token, c.now().UTC(), exitedOwner)
	for i := range reports {
		session, found, lookupErr := c.store.GetSession(ctx, reports[i].SessionID)
		if lookupErr == nil && found {
			reports[i].SessionDisplayName = strings.TrimSpace(session.DisplayName)
		}
	}
	batchID := ""
	if len(reports) > 0 {
		batchID = reports[0].DeliveryBatchID
	}
	return PreparedBatch{ProjectID: projectID, Token: token, ID: batchID, Reports: reports}, err
}

// PreparePiggyback claims one live Chat-mode recipient's group. The caller
// must acknowledge only after its combined user message is durably accepted.
func (c *Coordinator) PreparePiggyback(ctx context.Context, target domain.SessionID) (PreparedBatch, error) {
	if c == nil || c.store == nil {
		return PreparedBatch{}, nil
	}
	rec, ok, err := c.store.GetSession(ctx, target)
	if err != nil || !ok {
		return PreparedBatch{}, err
	}
	if rec.IsTerminated || domain.NormalizeSessionMode(rec.Mode) != domain.SessionModeChat ||
		(rec.Kind != domain.KindOrchestrator && rec.Kind != domain.KindCoordinator) {
		return PreparedBatch{}, nil
	}
	root := rec.ID
	if rec.Kind != domain.KindOrchestrator {
		root = ""
		if live, found, lookupErr := c.activeOrchestrator(ctx, rec.ProjectID); lookupErr != nil {
			return PreparedBatch{}, lookupErr
		} else if found {
			root = live.ID
		}
	}
	return c.claim(ctx, rec.ProjectID, rec.ID, root, "")
}

// AcceptPiggyback acknowledges reports after the combined user turn is accepted.
func (c *Coordinator) AcceptPiggyback(ctx context.Context, batch PreparedBatch) error {
	if len(batch.Reports) == 0 {
		return nil
	}
	_, err := c.store.AcknowledgeReportBatch(ctx, batch.ProjectID, batch.Token, c.now().UTC())
	return err
}

// ReleasePiggyback makes a batch retryable when the combined turn was refused.
func (c *Coordinator) ReleasePiggyback(ctx context.Context, batch PreparedBatch, cause error) error {
	if len(batch.Reports) == 0 {
		return nil
	}
	return c.release(ctx, batch, cause)
}

func (c *Coordinator) release(ctx context.Context, batch PreparedBatch, cause error) error {
	_, releaseErr := c.store.ReleaseReportBatch(ctx, batch.ProjectID, batch.Token, c.now().UTC(), cause.Error())
	if releaseErr != nil {
		return errors.Join(cause, releaseErr)
	}
	return cause
}

const (
	chatReportOpen  = "<ao-worker-reports>"
	chatReportClose = "</ao-worker-reports>"
)

// AppendToUserMessage places reports after the user's text in one semantic turn.
// The envelope lets AO's Chat UI hide daemon-authored context while preserving
// the exact prompt accepted by the provider for retry and recovery.
func (b PreparedBatch) AppendToUserMessage(message string) string {
	if len(b.Reports) == 0 {
		return message
	}
	return message + "\n\n" + chatReportOpen + "\n" + b.Render() + "\n" + chatReportClose
}

// IdempotencyKey returns the stable durable delivery identity.
func (b PreparedBatch) IdempotencyKey() string {
	if b.ID != "" {
		return b.ID
	}
	ids := make([]string, len(b.Reports))
	for i := range b.Reports {
		ids[i] = b.Reports[i].ID
	}
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, "\x00")))
	return "report-batch:" + hex.EncodeToString(sum[:16])
}

// Render formats compact attributed report context for an orchestrator.
func (b PreparedBatch) Render() string {
	var out strings.Builder
	out.WriteString("Reports since your previous turn:")
	for _, report := range coalesceReports(b.Reports) {
		state := string(report.State)
		if state == "" {
			state = "information"
		}
		fmt.Fprintf(&out, "\n\n[%s] %s", state, reportSessionLink(report))
		if report.RepeatCount > 1 {
			fmt.Fprintf(&out, " (repeated %d times)", report.RepeatCount)
		}
		text := report.Note
		if text == "" {
			text = report.Message
		}
		if text != "" {
			out.WriteString("\n")
			out.WriteString(text)
		}
		for _, output := range report.Outputs {
			if output.Kind == domain.ReportOutputArtifact {
				if href := reportArtifactLink(report, output.Reference); href != "" {
					label := strings.ReplaceAll(output.Label, "[", "\\[")
					label = strings.ReplaceAll(label, "]", "\\]")
					label = strings.ReplaceAll(label, "\n", " ")
					if label == "" {
						label = output.Reference
					}
					fmt.Fprintf(&out, "\nartifact: [%s](%s)", label, href)
					continue
				}
			}
			fmt.Fprintf(&out, "\n%s: %s", output.Kind, output.Reference)
		}
	}
	return out.String()
}

func reportDisplayNameOrID(r domain.ReportRecord) string {
	if name := strings.TrimSpace(r.SessionDisplayName); name != "" {
		return name
	}
	return string(r.SessionID)
}

// reportSessionLink is the markdown attribution a standalone report shows.
// Piggybacked copies stay inside the hidden user-message envelope.
func reportSessionLink(r domain.ReportRecord) string {
	if r.HostLabel != "" {
		// The worker lives on another daemon: an ao:// link would name a local
		// session that does not exist. Say where to reach it instead.
		oneLine := strings.NewReplacer("\n", " ", "\r", " ")
		return fmt.Sprintf("%s (session %s on host %s)", oneLine.Replace(reportDisplayNameOrID(r)), oneLine.Replace(string(r.SessionID)), oneLine.Replace(r.HostLabel))
	}
	label := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "\n", " ", "\r", " ").Replace(reportDisplayNameOrID(r))
	href := reportSessionLinkURL(r)
	return "[" + label + "](" + href + ")"
}

func reportSessionLinkURL(r domain.ReportRecord) string {
	projectID := r.SourceProjectID
	if projectID == "" {
		projectID = string(r.ProjectID)
	}
	if r.HostID != "" {
		return "ao://hosts/" + url.PathEscape(r.HostID) + "/sessions/" + url.PathEscape(projectID) + "/" + url.PathEscape(string(r.SessionID))
	}
	return "ao://sessions/" + url.PathEscape(projectID) + "/" + url.PathEscape(string(r.SessionID))
}

func reportArtifactLink(r domain.ReportRecord, reference string) string {
	path := strings.TrimSpace(reference)
	if !safeReportArtifactPath(path) {
		return ""
	}
	return reportSessionLinkURL(r) + "/artifacts/" + escapeReportArtifactPath(path)
}

func safeReportArtifactPath(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") || strings.Contains(path, ":") {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if strings.TrimSpace(segment) == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func escapeReportArtifactPath(path string) string {
	segments := strings.Split(path, "/")
	for i := range segments {
		segments[i] = url.PathEscape(segments[i])
	}
	return strings.Join(segments, "/")
}

func (b PreparedBatch) hasState(state domain.ReportState) bool {
	for _, report := range b.Reports {
		if report.State == state {
			return true
		}
	}
	return false
}

func (b PreparedBatch) stuckWorkers() []domain.SessionID {
	seen := map[domain.SessionID]bool{}
	var out []domain.SessionID
	for _, report := range b.Reports {
		if report.State == domain.ReportStuck && !seen[report.SessionID] {
			seen[report.SessionID] = true
			out = append(out, report.SessionID)
		}
	}
	return out
}

func coalesceReports(in []domain.ReportRecord) []domain.ReportRecord {
	out := make([]domain.ReportRecord, 0, len(in))
	indexes := map[string]int{}
	for _, report := range in {
		key := reportKey(report)
		if report.State == domain.ReportStuck {
			if i, ok := indexes[key]; ok {
				out[i].RepeatCount += report.RepeatCount
				continue
			}
			indexes[key] = len(out)
		}
		out = append(out, report)
	}
	return out
}

func reportKey(report domain.ReportRecord) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\x00%s\x00%s\x00%s\x00%s", report.HostLabel, report.SessionID, report.State, report.Note, report.Message)
	for _, output := range report.Outputs {
		fmt.Fprintf(&b, "\x00%s\x00%s\x00%s", output.Kind, output.Reference, output.Label)
	}
	return b.String()
}
