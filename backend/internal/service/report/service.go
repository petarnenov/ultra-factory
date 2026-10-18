package report

import (
	"context"
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
	store     Store
	now       func() time.Time
	newID     func() string
	onCreated func(domain.ReportRecord)
	artifacts ArtifactRecorder
}

// Deps configures a Service. Artifacts is optional.
type Deps struct {
	Store     Store
	Now       func() time.Time
	NewID     func() string
	OnCreated func(domain.ReportRecord)
	Artifacts ArtifactRecorder
}

// New constructs a report Service.
func New(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.NewID == nil {
		d.NewID = func() string { return "rpt_" + uuid.NewString() }
	}
	return &Service{store: d.Store, now: d.Now, newID: d.NewID, onCreated: d.OnCreated, artifacts: d.Artifacts}
}

// ListProject returns persisted report facts ordered by created_at then id,
// with each report's outputs in persisted position order. It never claims,
// acknowledges, schedules, or otherwise mutates report delivery state.
func (s *Service) ListProject(ctx context.Context, projectID domain.ProjectID) ([]domain.ReportRecord, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("report: store is required")
	}
	if projectID == "" {
		return nil, apierr.Invalid("REPORT_PROJECT_REQUIRED", "Project id is required", nil)
	}
	reader, ok := s.store.(interface {
		ListReportsByProject(context.Context, domain.ProjectID) ([]domain.ReportRecord, error)
	})
	if !ok {
		return nil, errors.New("report: project reader is required")
	}
	reports, err := reader.ListReportsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	type sessionSource struct {
		found   bool
		name    string
		harness domain.AgentHarness
	}
	sources := map[domain.SessionID]sessionSource{}
	for i := range reports {
		if reports[i].HostID == "" {
			reports[i].SourceProjectID = string(reports[i].ProjectID)
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
			return reports[i].ID < reports[j].ID
		}
		return reports[i].CreatedAt.Before(reports[j].CreatedAt)
	})
	return reports, nil
}

// MaxAcknowledgeReports bounds one acknowledgement request.
const MaxAcknowledgeReports = 200

// MaxPulledReportBytes bounds one pending-reports response well below what a
// pulling daemon reads, so a page of large reports cannot make it fail to
// decode the response and stall the worker for good.
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

// ListDuePending returns one worker's reports that are still pending and due
// now, in creation order, for a peer daemon that delivers them to its own
// orchestrator. Due keeps the local batching and done-settlement delays;
// morePending reports pending reports that are not due yet or beyond one
// acknowledgement's worth, so the peer keeps polling a worker that has
// already ended. It never claims or mutates delivery
// state; AcknowledgePulled does.
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
	// A report is delivered to its project's orchestrator. A standalone worker
	// has no project, so its report has no recipient and would otherwise fail
	// the record's invariants as an internal error.
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
