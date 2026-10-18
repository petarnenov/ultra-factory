package report

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

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
	selfHostID   string
	selfHostName string
}

// Deps configures a Service. Artifacts is optional.
type Deps struct {
	Store        Store
	Now          func() time.Time
	NewID        func() string
	OnCreated    func(domain.ReportRecord)
	Artifacts    ArtifactRecorder
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
	return &Service{store: d.Store, now: d.Now, newID: d.NewID, onCreated: d.OnCreated, artifacts: d.Artifacts, selfHostID: d.SelfHostID, selfHostName: d.SelfHostName}
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
	page, err := s.ListProjectPage(ctx, projectID, ListProjectFilter{})
	if err != nil {
		return nil, err
	}
	return page.Reports, nil
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
		limit = 100000
	}
	if limit > MaxListProjectLimit && filter.Limit > 0 {
		limit = MaxListProjectLimit
	}
	fetchLimit := limit
	if filter.Limit > 0 {
		fetchLimit = limit + 1
	}
	var reports []domain.ReportRecord
	pager, isPager := s.store.(interface {
		ListReportsByProjectPage(context.Context, domain.ProjectID, time.Time, string, string, string, int) ([]domain.ReportRecord, error)
	})
	if isPager {
		reports, err = pager.ListReportsByProjectPage(ctx, projectID, cursorTime, cursorHost, cursorSession, cursorID, fetchLimit)
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
				(report.CreatedAt.Equal(cursorTime) && report.HostID == cursorHost && string(report.SessionID) == cursorSession && strings.Compare(string(report.ID), cursorID) < 0) {
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

// ListDuePending returns one worker's pending, due reports.
// It preserves batching and never mutates delivery state.
func (s *Service) ListDuePending(ctx context.Context, sessionID domain.SessionID) (due []domain.ReportRecord, morePending bool, err error) {
	if s == nil || s.store == nil {
		return nil, false, errors.New("report: store is required")
	}
	if err := s.pullableWorker(ctx, sessionID); err != nil {
		return nil, false, err
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
				return nil, false, err
			}
		}
	}
	reader, ok := s.store.(interface {
		ListReportsBySession(context.Context, domain.SessionID) ([]domain.ReportRecord, error)
	})
	if !ok {
		return nil, false, errors.New("report: session reader is required")
	}
	reports, err := reader.ListReportsBySession(ctx, sessionID)
	if err != nil {
		return nil, false, err
	}
	if session, found, lookupErr := s.store.GetSession(ctx, sessionID); lookupErr == nil && found {
		for i := range reports {
			reports[i].Agent = string(session.Harness)
			reports[i].SessionDisplayName = strings.TrimSpace(session.DisplayName)
			reports[i].SourceProjectID = string(session.ProjectID)
		}
	}
	now := s.now().UTC()
	budget := 0
	due = make([]domain.ReportRecord, 0, len(reports))
	for _, report := range reports {
		if report.DeliveryState != domain.ReportPending {
			continue
		}
		if report.AvailableAt.After(now) {
			morePending = true
			continue
		}
		size := encodedReportBytes(report)
		if len(due) == MaxAcknowledgeReports || (len(due) > 0 && budget+size > MaxPulledReportBytes) {
			// Stop rather than skip, so a later small report never overtakes
			// an earlier large one.
			morePending = true
			break
		}
		budget += size
		due = append(due, report)
	}
	return due, morePending, nil
}

// AcknowledgePulled settles reports a peer daemon pulled and delivered. Only
// the named worker's still-pending reports change; it returns those IDs.
func (s *Service) AcknowledgePulled(ctx context.Context, sessionID domain.SessionID, ids []string) ([]string, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("report: store is required")
	}
	if len(ids) == 0 || len(ids) > MaxAcknowledgeReports {
		return nil, apierr.Invalid("INVALID_REPORT_IDS", "Between 1 and 200 report ids are required", nil)
	}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			return nil, apierr.Invalid("INVALID_REPORT_IDS", "Report ids must not be empty", nil)
		}
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

// MaxAcknowledgeReports bounds one acknowledgement request.
const MaxAcknowledgeReports = 200

// MaxPulledReportBytes bounds pending reports per response.
// It prevents large pages from failing to decode.
const MaxPulledReportBytes = 2 << 20

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

// ListDuePending returns one worker's pending, due reports.
// It preserves local batching and settlement delays;
