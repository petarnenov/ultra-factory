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
	ClaimPendingReportBatch(context.Context, domain.ProjectID, string, time.Time) ([]domain.ReportRecord, error)
	AcknowledgeReportBatch(context.Context, domain.ProjectID, string, time.Time) (int, error)
	ReleaseReportBatch(context.Context, domain.ProjectID, string, time.Time, string) (int, error)
	DeferReportBatch(context.Context, domain.ProjectID, string, time.Time, string) (int, error)
	AcquireReportInterrupt(context.Context, domain.SessionID, time.Time, time.Duration) (bool, time.Time, error)
}

// Delivery submits accepted semantic context and interrupts active work.
type Delivery interface {
	Submit(context.Context, domain.SessionID, string, string) error
	Interrupt(context.Context, domain.SessionID) error
}

// Coordinator delivers durable report batches to active orchestrators.
type Coordinator struct {
	store      DeliveryStore
	delivery   Delivery
	now        func() time.Time
	newToken   func() string
	retryDelay time.Duration
	wake       chan struct{}
	mu         sync.Mutex
}

// CoordinatorDeps configures durable scheduling and delivery.
type CoordinatorDeps struct {
	Store      DeliveryStore
	Delivery   Delivery
	Now        func() time.Time
	NewToken   func() string
	RetryDelay time.Duration
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
	return &Coordinator{store: d.Store, delivery: d.Delivery, now: d.Now, newToken: d.NewToken, retryDelay: d.RetryDelay, wake: make(chan struct{}, 1)}
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

// RunDue performs bounded due delivery and returns the delay until the next
// durable deadline. Tests call it directly with fake time.
func (c *Coordinator) RunDue(ctx context.Context) (time.Duration, error) {
	if c == nil || c.store == nil || c.delivery == nil {
		return -1, errors.New("report coordinator dependencies are required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// A batch claimed in this pass that acknowledged nothing waits for its
	// deadline instead of being claimed again, so a deferral cannot spin.
	attempted := map[string]bool{}
	for {
		pending, err := c.store.ListPendingReportSchedule(ctx, 1)
		if err != nil {
			return c.retryDelay, err
		}
		if len(pending) == 0 {
			return -1, nil
		}
		now := c.now().UTC()
		key := batchKey(pending[0])
		flushing := pending[0].AvailableAt.After(now)
		if flushing && (attempted[key] || !c.terminatedWorker(ctx, pending[0].SessionID)) {
			return pending[0].AvailableAt.Sub(now), nil
		}
		// A terminated worker cannot report again, so a not-yet-due report of
		// its queue is flushed now instead of trickling in after its deadline.
		consumed, err := c.deliverProject(ctx, pending[0].ProjectID)
		if err != nil {
			return c.retryDelay, err
		}
		if consumed {
			continue
		}
		if attempted[key] {
			return c.retryDelay, nil
		}
		attempted[key] = true
	}
}

// terminatedWorker reports a worker that will never produce another report.
func (c *Coordinator) terminatedWorker(ctx context.Context, id domain.SessionID) bool {
	session, found, err := c.store.GetSession(ctx, id)
	return err == nil && found && session.IsTerminated
}

// batchKey names the durable batch a pending report belongs to. A report with
// no batch id yet gets the one a claim would derive from its own id.
func batchKey(r domain.ReportRecord) string {
	batch := r.DeliveryBatchID
	if batch == "" {
		batch = "report-batch:" + r.ID
	}
	return string(r.ProjectID) + "\x00" + batch
}

// deliverProject reports whether it acknowledged a batch, which is the only
// outcome that makes another look at the schedule worthwhile.
func (c *Coordinator) deliverProject(ctx context.Context, projectID domain.ProjectID) (bool, error) {
	target, ok, err := c.activeOrchestrator(ctx, projectID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("project %s has no active orchestrator", projectID)
	}
	batch, err := c.claim(ctx, projectID)
	if err != nil || len(batch.Reports) == 0 {
		return false, err
	}
	if batch.hasState(domain.ReportStuck) {
		interrupted := false
		var nextInterrupt time.Time
		for _, worker := range batch.stuckWorkers() {
			acquired, next, acquireErr := c.store.AcquireReportInterrupt(ctx, worker, c.now().UTC(), ReportInterruptWindow)
			if acquireErr != nil {
				return false, c.release(ctx, batch, acquireErr)
			}
			if acquired {
				interrupted = true
				if interruptErr := c.delivery.Interrupt(ctx, target.ID); interruptErr != nil {
					return false, c.release(ctx, batch, interruptErr)
				}
			} else if nextInterrupt.IsZero() || next.Before(nextInterrupt) {
				nextInterrupt = next
			}
		}
		if !interrupted && !batch.hasState(domain.ReportNeedsInput) {
			_, deferErr := c.store.DeferReportBatch(ctx, batch.ProjectID, batch.Token, nextInterrupt, "repeated stuck report coalesced inside interrupt window")
			return false, deferErr
		}
	}
	if err := c.delivery.Submit(ctx, target.ID, batch.Render(), batch.IdempotencyKey()); err != nil {
		if !errors.Is(err, domain.ErrSemanticAcceptanceTimeout) {
			slog.Warn("report delivery failed", "delivery_id", batch.IdempotencyKey(), "err", err)
			return false, c.release(ctx, batch, err)
		}
		// The prompt was written; only its acceptance hook is late. Re-arming
		// the batch re-pastes it on every retry, so it is consumed once here.
		slog.Warn("report delivery unconfirmed; batch consumed", "delivery_id", batch.IdempotencyKey(), "err", err)
		if ackErr := c.consume(ctx, batch); ackErr != nil {
			return false, errors.Join(err, ackErr)
		}
		return true, nil
	}
	if err := c.consume(ctx, batch); err != nil {
		return false, err
	}
	return true, nil
}

// consume acknowledges a submitted batch, so no later pass sends it again.
func (c *Coordinator) consume(ctx context.Context, batch PreparedBatch) error {
	_, err := c.store.AcknowledgeReportBatch(ctx, batch.ProjectID, batch.Token, c.now().UTC())
	return err
}

// ErrNoActiveOrchestrator reports a project with no orchestrator to deliver to.
var ErrNoActiveOrchestrator = errors.New("project has no active orchestrator")

// DeliverExternal submits reports pulled from a peer daemon's worker to the
// project's active orchestrator in one turn. The reports are not stored here;
// the caller records them as delivered only after this returns nil.
func (c *Coordinator) DeliverExternal(ctx context.Context, projectID domain.ProjectID, reports []domain.ReportRecord) error {
	if c == nil || c.store == nil || c.delivery == nil {
		return errors.New("report coordinator dependencies are required")
	}
	if len(reports) == 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	target, ok, err := c.activeOrchestrator(ctx, projectID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoActiveOrchestrator, projectID)
	}
	batch := PreparedBatch{ProjectID: projectID, Reports: reports}
	return c.delivery.Submit(ctx, target.ID, batch.Render(), batch.IdempotencyKey())
}

func (c *Coordinator) activeOrchestrator(ctx context.Context, projectID domain.ProjectID) (domain.SessionRecord, bool, error) {
	recs, err := c.store.ListSessions(ctx, projectID)
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

func (c *Coordinator) claim(ctx context.Context, projectID domain.ProjectID) (PreparedBatch, error) {
	token := c.newToken()
	reports, err := c.store.ClaimPendingReportBatch(ctx, projectID, token, c.now().UTC())
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

// PreparePiggyback claims pending reports for the active Chat orchestrator.
// The caller must acknowledge only after its user message is durably accepted.
func (c *Coordinator) PreparePiggyback(ctx context.Context, target domain.SessionID) (PreparedBatch, error) {
	if c == nil || c.store == nil {
		return PreparedBatch{}, nil
	}
	rec, ok, err := c.store.GetSession(ctx, target)
	if err != nil || !ok {
		return PreparedBatch{}, err
	}
	if rec.Kind != domain.KindOrchestrator || rec.IsTerminated || domain.NormalizeSessionMode(rec.Mode) != domain.SessionModeChat {
		return PreparedBatch{}, nil
	}
	return c.claim(ctx, rec.ProjectID)
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
