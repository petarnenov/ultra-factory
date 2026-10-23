package report

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/requestscope"
)

// Store is the report service's persistence and ownership boundary.
type Store interface {
	GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
	CreateReport(context.Context, domain.ReportRecord) (domain.ReportRecord, error)
}

// CreateInput is the validated caller payload before AO-derived ownership and deadlines.
type CreateInput struct {
	SessionID domain.SessionID
	State     domain.ReportState
	Note      string
	Message   string
	Outputs   []domain.ReportOutput
}

// ArtifactRecorder shows a reported artifact in the worker's chat thread. It
// is best effort: it skips what it cannot show and never fails the report.
type ArtifactRecorder interface {
	RecordReportedArtifact(ctx context.Context, id domain.SessionID, reference string)
}

// Service validates and creates durable worker reports.
type Service struct {
	store        Store
	now          func() time.Time
	newID        func() string
	onCreated    func(domain.ReportRecord)
	artifacts    ArtifactRecorder
	claims       PROutcomeClaims
	selfHostID   string
	selfHostName string
}

// Deps configures a Service. Artifacts is optional; Claims is required only
// for PR outcome announcements.
type Deps struct {
	Store        Store
	Now          func() time.Time
	NewID        func() string
	OnCreated    func(domain.ReportRecord)
	Artifacts    ArtifactRecorder
	Claims       PROutcomeClaims
	SelfHostID   string
	SelfHostName string
}

// New constructs a report Service.
func New(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.NewID == nil {
		d.NewID = func() string { return "rpt_" + uuid.NewString() }
	}
	return &Service{store: d.Store, now: d.Now, newID: d.NewID, onCreated: d.OnCreated, artifacts: d.Artifacts, claims: d.Claims, selfHostID: d.SelfHostID, selfHostName: d.SelfHostName}
}

// DefaultListProjectLimit keeps the first Reports panel page bounded.
const DefaultListProjectLimit = 20

// MaxListProjectLimit keeps every Reports panel page bounded.
const MaxListProjectLimit = 100

// ListProjectFilter configures one Reports panel page.
type ListProjectFilter struct {
	Limit  int
	Cursor string
}

// ListProjectPage is one stable newest-first Reports panel page.
type ListProjectPage struct {
	Reports    []domain.ReportRecord
	NextCursor string
}

// SetSelfHostID records this daemon's identity for local reports.
func (s *Service) SetSelfHostID(id string) {
	if s != nil {
		s.selfHostID = strings.TrimSpace(id)
	}
}

// SetSelfHostName records this daemon's human-readable machine name.
func (s *Service) SetSelfHostName(name string) {
	if s != nil {
		s.selfHostName = strings.TrimSpace(name)
	}
}

// ListProject returns persisted report facts ordered newest first.
// It is the compatibility entry point for callers that do not paginate.
func (s *Service) ListProject(ctx context.Context, projectID domain.ProjectID) ([]domain.ReportRecord, error) {
	var reports []domain.ReportRecord
	cursor := ""
	for {
		page, err := s.ListProjectPage(ctx, projectID, ListProjectFilter{Limit: MaxListProjectLimit, Cursor: cursor})
		if err != nil {
			return nil, err
		}
		reports = append(reports, page.Reports...)
		if page.NextCursor == "" {
			return reports, nil
		}
		cursor = page.NextCursor
	}
}

// ListProjectPage returns one stable newest-first page.
// It never mutates report delivery state.
func (s *Service) ListProjectPage(ctx context.Context, projectID domain.ProjectID, filter ListProjectFilter) (ListProjectPage, error) {
	if s == nil || s.store == nil {
		return ListProjectPage{}, errors.New("report: store is required")
	}
	if projectID == "" {
		return ListProjectPage{}, apierr.Invalid("REPORT_PROJECT_REQUIRED", "Project id is required", nil)
	}
	cursorTime, cursorHost, cursorSession, cursorID, err := decodeReportCursor(filter.Cursor)
	if err != nil {
		return ListProjectPage{}, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultListProjectLimit
	}
	if limit > MaxListProjectLimit && filter.Limit > 0 {
		limit = MaxListProjectLimit
	}
	var reports []domain.ReportRecord
	pager, isPager := s.store.(interface {
		ListReportsByProjectPage(context.Context, domain.ProjectID, time.Time, string, string, string, int) ([]domain.ReportRecord, error)
	})
	if isPager {
		reports, err = pager.ListReportsByProjectPage(ctx, projectID, cursorTime, cursorHost, cursorSession, cursorID, limit)
		if err != nil {
			return ListProjectPage{}, err
		}
	} else {
		reader, ok := s.store.(interface {
			ListReportsByProject(context.Context, domain.ProjectID) ([]domain.ReportRecord, error)
		})
		if !ok {
			return ListProjectPage{}, errors.New("report: project reader is required")
		}
		all, err := reader.ListReportsByProject(ctx, projectID)
		if err != nil {
			return ListProjectPage{}, err
		}
		for _, report := range all {
			if cursorTime.IsZero() ||
				report.CreatedAt.Before(cursorTime) ||
				(report.CreatedAt.Equal(cursorTime) && report.HostID < cursorHost) ||
				(report.CreatedAt.Equal(cursorTime) && report.HostID == cursorHost && string(report.SessionID) < cursorSession) ||
				(report.CreatedAt.Equal(cursorTime) && report.HostID == cursorHost && string(report.SessionID) == cursorSession && report.ID < cursorID) {
				reports = append(reports, report)
			}
		}
	}
	for i := range reports {
		if reports[i].SourceProjectID == "" {
			reports[i].SourceProjectID = string(reports[i].ProjectID)
		}
	}
	type sessionSource struct {
		found   bool
		name    string
		harness domain.AgentHarness
	}
	sources := map[domain.SessionID]sessionSource{}
	for i := range reports {
		if reports[i].SourceProjectID == "" {
			reports[i].SourceProjectID = string(reports[i].ProjectID)
		}
		if reports[i].HostID == "" {
			if reports[i].HostLabel == "" {
				if s.selfHostName != "" {
					reports[i].HostLabel = s.selfHostName
				} else if s.selfHostID != "" {
					reports[i].HostLabel = s.selfHostID
				}
			}
			source, cached := sources[reports[i].SessionID]
			if !cached {
				session, found, lookupErr := s.store.GetSession(ctx, reports[i].SessionID)
				if lookupErr == nil && found {
					source = sessionSource{found: true, name: strings.TrimSpace(session.DisplayName), harness: session.Harness}
				}
				sources[reports[i].SessionID] = source
			}
			if source.found {
				reports[i].SessionDisplayName = source.name
				reports[i].Agent = string(source.harness)
			}
		}
	}
	sort.SliceStable(reports, func(i, j int) bool {
		if reports[i].CreatedAt.Equal(reports[j].CreatedAt) {
			if reports[i].HostID != reports[j].HostID {
				return reports[i].HostID > reports[j].HostID
			}
			if reports[i].SessionID != reports[j].SessionID {
				return reports[i].SessionID > reports[j].SessionID
			}
			return reports[i].ID > reports[j].ID
		}
		return reports[i].CreatedAt.After(reports[j].CreatedAt)
	})
	hasMore := false
	if filter.Limit > 0 && len(reports) > limit {
		hasMore = true
		reports = reports[:limit]
	}
	page := ListProjectPage{Reports: reports}
	if hasMore && len(reports) > 0 {
		page.NextCursor = encodeReportCursor(reports[len(reports)-1])
	}
	return page, nil
}

func encodeReportCursor(rec domain.ReportRecord) string {
	value := rec.CreatedAt.UTC().Format(time.RFC3339Nano) + "\n" + rec.HostID + "\n" + string(rec.SessionID) + "\n" + rec.ID
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func decodeReportCursor(raw string) (time.Time, string, string, string, error) {
	if raw == "" {
		return time.Time{}, "", "", "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, "", "", "", apierr.Invalid("INVALID_REPORT_CURSOR", "Report cursor is invalid", nil)
	}
	createdAtRaw, rest, ok := strings.Cut(string(decoded), "\n")
	if !ok {
		return time.Time{}, "", "", "", apierr.Invalid("INVALID_REPORT_CURSOR", "Report cursor is invalid", nil)
	}
	hostID, rest, ok := strings.Cut(rest, "\n")
	if !ok {
		return time.Time{}, "", "", "", apierr.Invalid("INVALID_REPORT_CURSOR", "Report cursor is invalid", nil)
	}
	sessionID, id, ok := strings.Cut(rest, "\n")
	if !ok || id == "" {
		return time.Time{}, "", "", "", apierr.Invalid("INVALID_REPORT_CURSOR", "Report cursor is invalid", nil)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return time.Time{}, "", "", "", apierr.Invalid("INVALID_REPORT_CURSOR", "Report cursor is invalid", nil)
	}
	return createdAt.UTC(), hostID, sessionID, id, nil
}

// PullOptions identifies one pull. An empty PullerID is the legacy pull: the
// first daemon to acknowledge a report settles it for every daemon.
type PullOptions struct {
	// PullerID is the pulling daemon's instance ID.
	PullerID string
	// ExpiredSeen is the ExpiredTotal the puller last logged.
	ExpiredSeen int64
}

// PullResult is one worker's pending reports for one pull.
type PullResult struct {
	Due         []domain.ReportRecord
	MorePending bool
	// ExpiredTotal is the worker's monotonic count of reports that expired
	// before this puller acknowledged them. Zero for a legacy pull.
	ExpiredTotal int64
	// ExpiredBeforePull is ExpiredTotal minus the ExpiredSeen the puller sent.
	ExpiredBeforePull int64
}

// ListDuePending returns one worker's pending, due reports. A terminated
// worker's queue is due at once; batching holds, delivery state is unchanged.
func (s *Service) ListDuePending(ctx context.Context, sessionID domain.SessionID) (due []domain.ReportRecord, morePending bool, err error) {
	result, err := s.PullFor(ctx, sessionID, PullOptions{})
	return result.Due, result.MorePending, err
}

// PullFor returns one worker's reports due for the puller in opts. A puller
// with an ID receives every pending report it has not acknowledged itself.
func (s *Service) PullFor(ctx context.Context, sessionID domain.SessionID, opts PullOptions) (PullResult, error) {
	if s == nil || s.store == nil {
		return PullResult{}, errors.New("report: store is required")
	}
	if err := s.pullableWorker(ctx, sessionID); err != nil {
		return PullResult{}, err
	}
	// A peer daemon pulling this worker's reports runs its orchestrator. Mark
	// the worker as remotely started so this machine refuses an orchestrator
	// of its own, which would claim the reports first. This also covers
	// workers started before `ao spawn --host` sent remoteSpawn.
	if requestscope.IsLAN(ctx) {
		if marker, ok := s.store.(interface {
			MarkRemoteSpawned(context.Context, domain.SessionID, time.Time) error
		}); ok {
			if err := marker.MarkRemoteSpawned(ctx, sessionID, s.now().UTC()); err != nil {
				return PullResult{}, err
			}
		}
	}
	var (
		expired int64
		acked   map[string]bool
	)
	if opts.PullerID != "" {
		if err := validPullerID(opts.PullerID); err != nil {
			return PullResult{}, err
		}
		if err := s.compactReports(ctx, sessionID); err != nil {
			return PullResult{}, err
		}
		puller, ok := s.store.(interface {
			TouchReportPuller(context.Context, domain.SessionID, string, time.Time) (int64, map[string]bool, error)
		})
		if !ok {
			return PullResult{}, errors.New("report: puller store is required")
		}
		var err error
		expired, acked, err = puller.TouchReportPuller(ctx, sessionID, opts.PullerID, s.now().UTC())
		if err != nil {
			return PullResult{}, err
		}
	}
	reader, ok := s.store.(interface {
		ListReportsBySession(context.Context, domain.SessionID) ([]domain.ReportRecord, error)
	})
	if !ok {
		return PullResult{}, errors.New("report: session reader is required")
	}
	reports, err := reader.ListReportsBySession(ctx, sessionID)
	if err != nil {
		return PullResult{}, err
	}
	terminated := false
	if session, found, lookupErr := s.store.GetSession(ctx, sessionID); lookupErr == nil && found {
		terminated = session.IsTerminated
		for i := range reports {
			reports[i].Agent = string(session.Harness)
			reports[i].SessionDisplayName = strings.TrimSpace(session.DisplayName)
			reports[i].SourceProjectID = string(session.ProjectID)
		}
	}
	now := s.now().UTC()
	budget := 0
	result := PullResult{ExpiredTotal: expired, ExpiredBeforePull: max(expired-opts.ExpiredSeen, 0)}
	result.Due = make([]domain.ReportRecord, 0, len(reports))
	for _, report := range reports {
		if report.DeliveryState != domain.ReportPending || acked[report.ID] {
			continue
		}
		if report.AvailableAt.After(now) && !terminated {
			result.MorePending = true
			continue
		}
		size := encodedReportBytes(report)
		if len(result.Due) == MaxAcknowledgeReports || (len(result.Due) > 0 && budget+size > MaxPulledReportBytes) {
			// Stop rather than skip, so a later small report never overtakes
			// an earlier large one.
			result.MorePending = true
			break
		}
		budget += size
		result.Due = append(result.Due, report)
	}
	return result, nil
}

// compactReports expires the worker's pending reports past PullRetention or
// beyond MaxReportsPerSession. A store without compaction keeps them all.
func (s *Service) compactReports(ctx context.Context, sessionID domain.SessionID) error {
	compactor, ok := s.store.(interface {
		CompactExpiredReports(context.Context, domain.SessionID, time.Time, int, time.Time) (int64, error)
	})
	if !ok {
		return nil
	}
	now := s.now().UTC()
	_, err := compactor.CompactExpiredReports(ctx, sessionID, now.Add(-PullRetention), MaxReportsPerSession, now)
	return err
}

// AcknowledgePulled settles reports a peer daemon pulled and delivered. Only
// the named worker's still-pending reports change; it returns those IDs.
func (s *Service) AcknowledgePulled(ctx context.Context, sessionID domain.SessionID, ids []string) ([]string, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("report: store is required")
	}
	if err := validateAckIDs(ids); err != nil {
		return nil, err
	}
	if err := s.pullableWorker(ctx, sessionID); err != nil {
		return nil, err
	}
	acker, ok := s.store.(interface {
		AcknowledgePendingSessionReports(context.Context, domain.SessionID, []string, string, time.Time) ([]string, error)
	})
	if !ok {
		return nil, errors.New("report: acknowledgement store is required")
	}
	acknowledged, err := acker.AcknowledgePendingSessionReports(ctx, sessionID, ids, "pulled:"+uuid.NewString(), s.now().UTC())
	if err != nil {
		return nil, err
	}
	if acknowledged == nil {
		acknowledged = []string{}
	}
	return acknowledged, nil
}

// AcknowledgePulledFor records that pullerID received the named reports. The
// reports stay pending for every other puller and for the worker's own queue.
func (s *Service) AcknowledgePulledFor(ctx context.Context, sessionID domain.SessionID, ids []string, pullerID string) ([]string, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("report: store is required")
	}
	if err := validateAckIDs(ids); err != nil {
		return nil, err
	}
	if err := validPullerID(pullerID); err != nil {
		return nil, err
	}
	if err := s.pullableWorker(ctx, sessionID); err != nil {
		return nil, err
	}
	acker, ok := s.store.(interface {
		AcknowledgePendingSessionReportsFor(context.Context, domain.SessionID, []string, string, time.Time) ([]string, error)
	})
	if !ok {
		return nil, errors.New("report: acknowledgement store is required")
	}
	acknowledged, err := acker.AcknowledgePendingSessionReportsFor(ctx, sessionID, ids, pullerID, s.now().UTC())
	if err != nil {
		return nil, err
	}
	if acknowledged == nil {
		acknowledged = []string{}
	}
	return acknowledged, nil
}

func validateAckIDs(ids []string) error {
	if len(ids) == 0 || len(ids) > MaxAcknowledgeReports {
		return apierr.Invalid("INVALID_REPORT_IDS", "Between 1 and 200 report ids are required", nil)
	}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			return apierr.Invalid("INVALID_REPORT_IDS", "Report ids must not be empty", nil)
		}
	}
	return nil
}

// validPullerID accepts an instance ID of up to 128 printable, non-space bytes.
func validPullerID(pullerID string) error {
	if pullerID == "" || len(pullerID) > MaxPullerIDBytes || strings.ContainsFunc(pullerID, unicode.IsSpace) {
		return apierr.Invalid("INVALID_PULLER_ID", "Puller id must be 1-128 characters without spaces", nil)
	}
	return nil
}

func (s *Service) pullableWorker(ctx context.Context, sessionID domain.SessionID) error {
	if sessionID == "" {
		return apierr.Invalid("INVALID_REPORT_SESSION", "Worker session id is required", nil)
	}
	session, ok, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if !ok {
		return apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
	}
	if session.Kind != domain.KindWorker {
		return apierr.Invalid("REPORT_WORKER_REQUIRED", "Reports belong only to worker sessions", nil)
	}
	return nil
}

// Create validates ownership and persists one pending report. Reports are
// coordination claims and never mutate authoritative worker lifecycle state.
func (s *Service) Create(ctx context.Context, input CreateInput) (domain.ReportRecord, error) {
	if s == nil || s.store == nil {
		return domain.ReportRecord{}, errors.New("report: store is required")
	}
	if input.SessionID == "" {
		return domain.ReportRecord{}, apierr.Invalid("INVALID_REPORT_SESSION", "Worker session id is required", nil)
	}
	if err := domain.ValidateReportContent(input.State, input.Note, input.Message, input.Outputs); err != nil {
		return domain.ReportRecord{}, apierr.Invalid("INVALID_REPORT", "Report state, note, message, or outputs are invalid", nil)
	}
	session, ok, err := s.store.GetSession(ctx, input.SessionID)
	if err != nil {
		return domain.ReportRecord{}, err
	}
	if !ok {
		return domain.ReportRecord{}, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
	}
	if session.Kind != domain.KindWorker {
		return domain.ReportRecord{}, apierr.Invalid("REPORT_WORKER_REQUIRED", "Reports can only be created by worker sessions", nil)
	}
	// A terminated worker will never be read again, so accepting its report
	// would only queue one more delivery into a closed session stream.
	if session.IsTerminated {
		return domain.ReportRecord{}, apierr.Conflict("SESSION_TERMINATED", "Reports cannot be created by a terminated session", nil)
	}
	// A report is delivered to its project's orchestrator. A standalone worker
	// has no project, so it is rejected as invalid input, not an internal error.
	if session.ProjectID == "" {
		return domain.ReportRecord{}, apierr.Invalid("REPORT_PROJECT_REQUIRED", "Reports can only be created by sessions that belong to a project; standalone sessions have no orchestrator to receive them", nil)
	}

	now := s.now().UTC()
	var availableAt time.Time
	var settlementDeadline time.Time
	switch input.State {
	case domain.ReportDone:
		settlementDeadline = now.Add(domain.ReportSettlementWindow)
		availableAt = settlementDeadline
	case domain.ReportNeedsInput, domain.ReportStuck:
		availableAt = now
	default:
		availableAt = now.Add(domain.ReportBatchFallback)
	}
	rec := domain.ReportRecord{
		ID: s.newID(), SessionID: session.ID, ProjectID: session.ProjectID,
		State: input.State, Note: input.Note, Message: input.Message, Outputs: input.Outputs,
		CreatedAt: now, DeliveryState: domain.ReportPending, AvailableAt: availableAt,
		SettlementDeadline: settlementDeadline, RepeatCount: 1,
	}
	created, err := s.store.CreateReport(ctx, rec)
	if err != nil {
		return created, err
	}
	if s.onCreated != nil {
		s.onCreated(created)
	}
	// Synchronous on purpose: ao report --done is often the agent's last tool
	// call, so a goroutine would race the turn's completion and could drop the
	// artifact or put it on the next turn.
	if s.artifacts != nil {
		for _, output := range created.Outputs {
			if output.Kind == domain.ReportOutputArtifact {
				s.artifacts.RecordReportedArtifact(ctx, created.SessionID, output.Reference)
			}
		}
	}
	return created, nil
}

// createLifecycleReport files an AO-authored checkpoint that is due now. It
// accepts a terminated worker, because a PR can close after its worker ends.
func (s *Service) createLifecycleReport(ctx context.Context, worker domain.SessionRecord, note string) error {
	now := s.now().UTC()
	created, err := s.store.CreateReport(ctx, domain.ReportRecord{
		ID: s.newID(), SessionID: worker.ID, ProjectID: worker.ProjectID,
		State: domain.ReportCheckpoint, Note: note,
		CreatedAt: now, DeliveryState: domain.ReportPending, AvailableAt: now, RepeatCount: 1,
	})
	if err != nil {
		return err
	}
	if s.onCreated != nil {
		s.onCreated(created)
	}
	return nil
}

// MaxAcknowledgeReports bounds one acknowledgement request.
const MaxAcknowledgeReports = 200

// MaxPulledReportBytes bounds pending reports per response.
// It prevents large pages from failing to decode.
const MaxPulledReportBytes = 2 << 20

// PullRetention is how long a worker keeps a pending report for a puller to
// pull. Older reports expire, and each unacknowledged puller counts them.
const PullRetention = 7 * 24 * time.Hour

// MaxReportsPerSession caps a worker's retained reports; the oldest pending
// reports beyond it expire the same way PullRetention does.
const MaxReportsPerSession = 10000

// MaxPullerIDBytes bounds a puller instance ID.
const MaxPullerIDBytes = 128

// encodedReportBytes is a report's size as the pending route encodes it,
// including HTML escaping that turns one byte into six.
func encodedReportBytes(r domain.ReportRecord) int {
	type output struct{ Kind, Reference, Label string }
	outputs := make([]output, len(r.Outputs))
	for i, o := range r.Outputs {
		outputs[i] = output{string(o.Kind), o.Reference, o.Label}
	}
	encoded, err := json.Marshal(struct {
		ID, SessionID, ProjectID, State, Note, Message string
		Outputs                                        []output
	}{r.ID, string(r.SessionID), string(r.ProjectID), string(r.State), r.Note, r.Message, outputs})
	if err != nil {
		return MaxPulledReportBytes
	}
	return len(encoded) + 128
}
