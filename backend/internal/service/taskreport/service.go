// Package taskreport derives completed-task reports from durable facts. It
// persists nothing, and facts never recorded are reported as unavailable.
package taskreport

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	sessionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/session"
)

// Outcome is how a completed task ended.
type Outcome string

// Task outcomes describe the deliverable, not who stopped it: AO records no
// termination cause, so intent is not observable.
const (
	OutcomeMerged Outcome = "merged"
	OutcomeClosed Outcome = "closed"
	OutcomeFailed Outcome = "failed"
	// A terminated worker without a settled PR set is unfinished when it owns
	// a PR and no_pr when it recorded none.
	OutcomeUnfinished Outcome = "unfinished"
	OutcomeNoPR       Outcome = "no_pr"
)

// Valid reports whether o is a known outcome.
func (o Outcome) Valid() bool {
	switch o {
	case OutcomeMerged, OutcomeClosed, OutcomeFailed, OutcomeUnfinished, OutcomeNoPR:
		return true
	}
	return false
}

// Completeness says how much of a figure rests on recorded facts.
type Completeness string

// Completeness levels.
const (
	Complete    Completeness = "complete"
	Partial     Completeness = "partial"
	Unavailable Completeness = "unavailable"
)

// historyStartTolerance is how long after creation the first recorded row
// may appear and still count as covering the task from its start.
const historyStartTolerance = time.Minute

// Page limits.
const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// PRLink is one PR attributed to the task. Title and the diff totals describe
// the delivered change and stay zero until an observation carries them.
type PRLink struct {
	URL          string
	Number       int
	State        domain.PRState
	Title        string
	Additions    int
	Deletions    int
	ChangedFiles int
}

// TokenTotals are summed token counts. A nil count was never recorded.
// InputTokens includes CachedInputTokens.
type TokenTotals struct {
	InputTokens       *int64
	CachedInputTokens *int64
	OutputTokens      *int64
	Completeness      Completeness
}

// Summary is one completed task in a listing.
type Summary struct {
	SessionID      domain.SessionID
	ProjectID      domain.ProjectID
	Title          string
	IssueID        domain.IssueID
	PRs            []PRLink
	Outcome        Outcome
	CreatedAt      time.Time
	CompletedAt    *time.Time
	ElapsedSeconds *int64
	Harness        domain.AgentHarness
	Model          string
	Effort         string
	Tokens         TokenTotals
}

// ConfigSegment is the effective configuration from Since until the next one.
type ConfigSegment struct {
	Source  domain.ConfigChangeSource
	Harness string
	Model   string
	Effort  string
	Since   time.Time
}

// ConfigHistory is the task's recorded configuration over time.
type ConfigHistory struct {
	Completeness Completeness
	Segments     []ConfigSegment
}

// RunUsage is one run root's usage for one model.
type RunUsage struct {
	BindingID         int64
	Harness           domain.AgentHarness
	NativeRootID      string
	Model             string
	EventCount        int64
	InputTokens       *int64
	CachedInputTokens *int64
	OutputTokens      *int64
	Completeness      Completeness
}

// StageVisit is one stay in a stage. LeftAt is nil for an unfinished visit,
// whose duration runs to the report time.
type StageVisit struct {
	Stage           domain.KanbanColumn
	EnteredAt       time.Time
	LeftAt          *time.Time
	DurationSeconds int64
}

// StageTotal is the cumulative time and visit count for one stage.
type StageTotal struct {
	Stage         domain.KanbanColumn
	Seconds       int64
	Visits        int
	HasUnfinished bool
}

// StageHistory is the task's observed stage visits and per-stage totals.
type StageHistory struct {
	Completeness Completeness
	Visits       []StageVisit
	Totals       []StageTotal
}

// Report is one completed task's full execution report.
type Report struct {
	Summary
	Config ConfigHistory
	Runs   []RunUsage
	Stages StageHistory
}

// Page is one page of completed tasks.
type Page struct {
	Tasks      []Summary
	NextCursor string
}

// ListInput filters and pages a listing.
type ListInput struct {
	Outcome Outcome
	Limit   int
	Cursor  string
}

// Sessions is the session read model used for membership and PR facts.
type Sessions interface {
	List(ctx context.Context, filter sessionsvc.ListFilter) ([]domain.Session, error)
	Get(ctx context.Context, id domain.SessionID) (domain.Session, error)
}

// Store reads recorded history.
type Store interface {
	GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error)
	ListSessionStageTransitions(ctx context.Context, id domain.SessionID) ([]domain.StageTransition, error)
	ListSessionConfigHistory(ctx context.Context, id domain.SessionID) ([]domain.ConfigChange, error)
	ListSessionUsageRuns(ctx context.Context, id domain.SessionID) ([]domain.UsageRun, error)
}

// Deps configures a Service.
type Deps struct {
	Sessions Sessions
	Store    Store
	Now      func() time.Time
}

// Service builds completed-task listings and reports.
type Service struct {
	sessions Sessions
	store    Store
	now      func() time.Time
}

// New constructs a Service.
func New(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Service{sessions: d.Sessions, store: d.Store, now: d.Now}
}

// List returns the project's completed worker tasks, newest completion first.
// Tasks without a recorded completion time sort after those with one.
func (s *Service) List(ctx context.Context, projectID domain.ProjectID, in ListInput) (Page, error) {
	if in.Outcome != "" && !in.Outcome.Valid() {
		return Page{}, apierr.Invalid("INVALID_OUTCOME", "outcome must be merged, closed, failed, unfinished, or no_pr", nil)
	}
	limit := in.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	offset := 0
	if in.Cursor != "" {
		n, err := strconv.Atoi(in.Cursor)
		if err != nil || n < 0 {
			return Page{}, apierr.Invalid("INVALID_CURSOR", "cursor is not valid", nil)
		}
		offset = n
	}
	if err := s.requireProject(ctx, projectID); err != nil {
		return Page{}, err
	}
	sessions, err := s.sessions.List(ctx, sessionsvc.ListFilter{ProjectID: projectID})
	if err != nil {
		return Page{}, err
	}
	var done []Summary
	for _, sess := range sessions {
		if sess.Kind != domain.KindWorker || sess.ProjectID != projectID {
			continue
		}
		outcome, ok := classify(sess)
		if !ok || (in.Outcome != "" && outcome != in.Outcome) {
			continue
		}
		var transitions []domain.StageTransition
		if !hasPRCompletion(sess) {
			if transitions, err = s.store.ListSessionStageTransitions(ctx, sess.ID); err != nil {
				return Page{}, err
			}
		}
		done = append(done, summarize(sess, outcome, transitions))
	}
	sort.SliceStable(done, func(i, j int) bool {
		a, b := done[i], done[j]
		if (a.CompletedAt == nil) != (b.CompletedAt == nil) {
			return a.CompletedAt != nil
		}
		if a.CompletedAt != nil && !a.CompletedAt.Equal(*b.CompletedAt) {
			return a.CompletedAt.After(*b.CompletedAt)
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.After(b.CreatedAt)
		}
		return a.SessionID > b.SessionID
	})
	page := Page{Tasks: []Summary{}}
	if offset >= len(done) {
		return page, nil
	}
	end := min(offset+limit, len(done))
	page.Tasks = done[offset:end]
	if end < len(done) {
		page.NextCursor = strconv.Itoa(end)
	}
	for i := range page.Tasks {
		runs, err := s.store.ListSessionUsageRuns(ctx, page.Tasks[i].SessionID)
		if err != nil {
			return Page{}, err
		}
		_, page.Tasks[i].Tokens = usage(runs)
	}
	return page, nil
}

// Get returns one completed task's report. A session outside the project or
// not yet completed is reported as not found.
func (s *Service) Get(ctx context.Context, projectID domain.ProjectID, id domain.SessionID) (Report, error) {
	if err := s.requireProject(ctx, projectID); err != nil {
		return Report{}, err
	}
	sess, err := s.sessions.Get(ctx, id)
	if err != nil {
		return Report{}, err
	}
	outcome, ok := classify(sess)
	if sess.ProjectID != projectID || sess.Kind != domain.KindWorker || !ok {
		return Report{}, apierr.NotFound("COMPLETED_TASK_NOT_FOUND", "No completed task with this id in the project")
	}
	transitions, err := s.store.ListSessionStageTransitions(ctx, id)
	if err != nil {
		return Report{}, err
	}
	changes, err := s.store.ListSessionConfigHistory(ctx, id)
	if err != nil {
		return Report{}, err
	}
	usageRuns, err := s.store.ListSessionUsageRuns(ctx, id)
	if err != nil {
		return Report{}, err
	}
	report := Report{Summary: summarize(sess, outcome, transitions)}
	report.Runs, report.Tokens = usage(usageRuns)
	report.Config = configHistory(sess.CreatedAt, changes)
	report.Stages = stageHistory(sess.CreatedAt, report.CompletedAt, transitions, s.now().UTC())
	return report, nil
}

func (s *Service) requireProject(ctx context.Context, id domain.ProjectID) error {
	_, ok, err := s.store.GetProject(ctx, string(id))
	if err != nil {
		return err
	}
	if !ok {
		return apierr.NotFound("PROJECT_NOT_FOUND", "Unknown project")
	}
	return nil
}

// hasPRCompletion reports whether every attributed PR is merged or closed.
func hasPRCompletion(sess domain.Session) bool {
	if len(sess.PRs) == 0 {
		return false
	}
	for _, pr := range sess.PRs {
		if !pr.Merged && !pr.Closed {
			return false
		}
	}
	return true
}

func anyMerged(sess domain.Session) bool {
	for _, pr := range sess.PRs {
		if pr.Merged {
			return true
		}
	}
	return false
}

// classify decides whether a session is a completed task and how it ended.
// A live session with an open PR is not completed.
func classify(sess domain.Session) (Outcome, bool) {
	if sess.IsTaskPreparation {
		return "", false
	}
	if hasPRCompletion(sess) {
		if anyMerged(sess) {
			return OutcomeMerged, true
		}
		return OutcomeClosed, true
	}
	if !sess.IsTerminated {
		return "", false
	}
	if anyMerged(sess) {
		return OutcomeMerged, true
	}
	if sess.ProvisionState == domain.SessionProvisionFailed {
		return OutcomeFailed, true
	}
	if len(sess.PRs) > 0 {
		return OutcomeUnfinished, true
	}
	return OutcomeNoPR, true
}

func summarize(sess domain.Session, outcome Outcome, transitions []domain.StageTransition) Summary {
	out := Summary{
		SessionID: sess.ID, ProjectID: sess.ProjectID, Title: title(sess), IssueID: sess.IssueID,
		Outcome: outcome, CreatedAt: sess.CreatedAt.UTC(), Harness: sess.Harness,
		Model: sess.Metadata.Model, Effort: sess.Metadata.Effort, PRs: []PRLink{},
		Tokens: TokenTotals{Completeness: Unavailable},
	}
	for _, pr := range sess.PRs {
		state := domain.PRStateOpen
		switch {
		case pr.Merged:
			state = domain.PRStateMerged
		case pr.Closed:
			state = domain.PRStateClosed
		case pr.Draft:
			state = domain.PRStateDraft
		}
		out.PRs = append(out.PRs, PRLink{
			URL: pr.URL, Number: pr.Number, State: state, Title: pr.Title,
			Additions: pr.Additions, Deletions: pr.Deletions, ChangedFiles: pr.ChangedFiles,
		})
	}
	if at := completedAt(sess, transitions); !at.IsZero() {
		at = at.UTC()
		out.CompletedAt = &at
		if !sess.CreatedAt.IsZero() && !at.Before(sess.CreatedAt) {
			elapsed := int64(at.Sub(sess.CreatedAt) / time.Second)
			out.ElapsedSeconds = &elapsed
		}
	}
	return out
}

func title(sess domain.Session) string {
	if sess.DisplayName != "" {
		return sess.DisplayName
	}
	if sess.IssueID != "" {
		return string(sess.IssueID)
	}
	return string(sess.ID)
}

// completedAt is when the PR set settled, or for a task without one, when
// it was observed entering archive. Zero means it was never recorded.
func completedAt(sess domain.Session, transitions []domain.StageTransition) time.Time {
	if hasPRCompletion(sess) {
		var latest time.Time
		for _, pr := range sess.PRs {
			if pr.StateChangedAt.IsZero() {
				return time.Time{}
			}
			if pr.StateChangedAt.After(latest) {
				latest = pr.StateChangedAt
			}
		}
		return latest
	}
	for i := len(transitions) - 1; i >= 0; i-- {
		if transitions[i].Stage == domain.KanbanArchive {
			return transitions[i].EnteredAt
		}
	}
	return time.Time{}
}

func usage(rows []domain.UsageRun) ([]RunUsage, TokenTotals) {
	runs := make([]RunUsage, 0, len(rows))
	totals := TokenTotals{Completeness: Unavailable}
	if len(rows) == 0 {
		return runs, totals
	}
	var input, cached, output int64
	inputKnown, cachedKnown, outputKnown := false, false, false
	partial := false
	for _, row := range rows {
		run := RunUsage{
			BindingID: row.BindingID, Harness: row.Harness, NativeRootID: row.NativeRootID,
			Model: row.ModelID, EventCount: row.EventCount, Completeness: Complete,
		}
		if row.KnownInputCount > 0 {
			run.InputTokens = ptr(row.InputTokens)
			input += row.InputTokens
			inputKnown = true
		}
		if row.KnownCachedInputCount > 0 {
			run.CachedInputTokens = ptr(row.CachedInputTokens)
			cached += row.CachedInputTokens
			cachedKnown = true
		}
		if row.KnownOutputCount > 0 {
			run.OutputTokens = ptr(row.OutputTokens)
			output += row.OutputTokens
			outputKnown = true
		}
		if row.KnownInputCount < row.EventCount || row.KnownOutputCount < row.EventCount ||
			row.BindingState != domain.UsageBindingComplete {
			run.Completeness = Partial
			partial = true
		}
		runs = append(runs, run)
	}
	if inputKnown {
		totals.InputTokens = ptr(input)
	}
	if cachedKnown {
		totals.CachedInputTokens = ptr(cached)
	}
	if outputKnown {
		totals.OutputTokens = ptr(output)
	}
	switch {
	case !inputKnown && !outputKnown:
		totals.Completeness = Unavailable
	case partial || !inputKnown || !outputKnown:
		totals.Completeness = Partial
	default:
		totals.Completeness = Complete
	}
	return runs, totals
}

// configHistory folds recorded changes into effective configuration segments.
// A conversation override wins over the session value until cleared.
func configHistory(createdAt time.Time, changes []domain.ConfigChange) ConfigHistory {
	out := ConfigHistory{Completeness: Unavailable, Segments: []ConfigSegment{}}
	if len(changes) == 0 {
		return out
	}
	out.Completeness = Complete
	if changes[0].Source != domain.ConfigChangeSession ||
		changes[0].RecordedAt.After(createdAt.Add(historyStartTolerance)) {
		out.Completeness = Partial
	}
	var session, override domain.ConfigChange
	for _, change := range changes {
		if change.Source == domain.ConfigChangeConversation {
			override = change
		} else {
			session = change
		}
		seg := ConfigSegment{
			Source: change.Source, Harness: session.Harness,
			Model:  firstNonEmpty(override.Model, session.Model),
			Effort: firstNonEmpty(override.Effort, session.Effort),
			Since:  change.RecordedAt,
		}
		if seg.Harness == "" {
			seg.Harness = change.Harness
		}
		if n := len(out.Segments); n > 0 {
			last := out.Segments[n-1]
			if last.Harness == seg.Harness && last.Model == seg.Model && last.Effort == seg.Effort {
				continue
			}
		}
		out.Segments = append(out.Segments, seg)
	}
	return out
}

// stageHistory turns transitions into visits capped at completedAt when known;
// without it an open last visit runs to now and makes the history partial.
func stageHistory(createdAt time.Time, completedAt *time.Time, transitions []domain.StageTransition, now time.Time) StageHistory {
	out := StageHistory{Completeness: Unavailable, Visits: []StageVisit{}, Totals: []StageTotal{}}
	if len(transitions) == 0 {
		return out
	}
	out.Completeness = Complete
	if transitions[0].EnteredAt.After(createdAt.Add(historyStartTolerance)) {
		out.Completeness = Partial
	}
	totals := map[domain.KanbanColumn]*StageTotal{}
	for i, tr := range transitions {
		if tr.Stage == domain.KanbanArchive {
			continue
		}
		if completedAt != nil && !tr.EnteredAt.Before(*completedAt) {
			break
		}
		visit := StageVisit{Stage: tr.Stage, EnteredAt: tr.EnteredAt}
		end := now
		if i+1 < len(transitions) {
			left := transitions[i+1].EnteredAt
			visit.LeftAt = &left
			end = left
		}
		if completedAt != nil && (visit.LeftAt == nil || visit.LeftAt.After(*completedAt)) {
			left := *completedAt
			visit.LeftAt = &left
			end = left
		}
		if visit.LeftAt == nil {
			out.Completeness = Partial
		}
		if end.After(tr.EnteredAt) {
			visit.DurationSeconds = int64(end.Sub(tr.EnteredAt) / time.Second)
		}
		out.Visits = append(out.Visits, visit)
		total := totals[tr.Stage]
		if total == nil {
			total = &StageTotal{Stage: tr.Stage}
			totals[tr.Stage] = total
		}
		total.Seconds += visit.DurationSeconds
		total.Visits++
		total.HasUnfinished = total.HasUnfinished || visit.LeftAt == nil
	}
	for _, stage := range []domain.KanbanColumn{
		domain.KanbanBuilding, domain.KanbanValidating, domain.KanbanNeedsReview, domain.KanbanReady,
	} {
		if total := totals[stage]; total != nil {
			out.Totals = append(out.Totals, *total)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func ptr(v int64) *int64 { return &v }
