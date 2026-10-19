package report

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	coordownership "github.com/aoagents/agent-orchestrator/backend/internal/service/ownership"
)

// SummaryStore is the coordinator summary service's persistence boundary.
type SummaryStore interface {
	GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
	CreateCoordinatorSummary(context.Context, domain.CoordinatorSummary) (domain.CoordinatorSummary, bool, error)
	GetCoordinatorSummaryByKey(context.Context, domain.SessionID, string) (domain.CoordinatorSummary, bool, error)
	ListCoordinatorSummaries(context.Context, domain.SessionID, int64) ([]domain.CoordinatorSummary, error)
}

// SummaryInput is the caller payload of one coordinator summary submission.
type SummaryInput struct {
	CoordinatorSessionID domain.SessionID
	SubmissionKey        string
	Scope                string
	State                domain.CoordinatorSummaryState
	WorkerSessionIDs     []domain.SessionID
	Result               string
	EscalationReason     domain.EscalationReason
	EscalationNote       string
}

// SummaryService validates and persists coordinator summaries. It never
// touches worker reports: a summary is a separate record.
type SummaryService struct {
	store     SummaryStore
	now       func() time.Time
	newID     func() string
	onCreated func(domain.CoordinatorSummary)
}

// SummaryDeps configures a SummaryService.
type SummaryDeps struct {
	Store     SummaryStore
	Now       func() time.Time
	NewID     func() string
	OnCreated func(domain.CoordinatorSummary)
}

// NewSummaryService constructs a SummaryService.
func NewSummaryService(d SummaryDeps) *SummaryService {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.NewID == nil {
		d.NewID = func() string { return "csum_" + uuid.NewString() }
	}
	return &SummaryService{store: d.Store, now: d.Now, newID: d.NewID, onCreated: d.OnCreated}
}

// DefaultSummaryListLimit and MaxSummaryListLimit bound one history read.
const (
	DefaultSummaryListLimit = 20
	MaxSummaryListLimit     = 100
)

// Submit persists one summary for the root. The bool is false for a reused
// submission key: the original is returned and nothing new is delivered.
func (s *SummaryService) Submit(ctx context.Context, in SummaryInput) (domain.CoordinatorSummary, bool, error) {
	if s == nil || s.store == nil {
		return domain.CoordinatorSummary{}, false, errors.New("coordinator summary: store is required")
	}
	if in.CoordinatorSessionID == "" {
		return domain.CoordinatorSummary{}, false, apierr.Invalid("SUMMARY_COORDINATOR_REQUIRED", "Coordinator session id is required", nil)
	}
	coordinator, found, err := s.store.GetSession(ctx, in.CoordinatorSessionID)
	if err != nil {
		return domain.CoordinatorSummary{}, false, err
	}
	if !found {
		return domain.CoordinatorSummary{}, false, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
	}
	if coordinator.Kind != domain.KindCoordinator || coordinator.ProjectID == "" {
		return domain.CoordinatorSummary{}, false, apierr.Forbidden("SUMMARY_COORDINATOR_REQUIRED", "Only a project coordinator may submit a summary")
	}
	key := strings.TrimSpace(in.SubmissionKey)
	if key == "" {
		return domain.CoordinatorSummary{}, false, apierr.Invalid("INVALID_SUMMARY", "A submission key is required", nil)
	}
	// Replay first: a retry after the coordinator was terminated or a worker
	// was reassigned still resolves to the original summary.
	if existing, ok, err := s.store.GetCoordinatorSummaryByKey(ctx, coordinator.ID, key); err != nil {
		return domain.CoordinatorSummary{}, false, err
	} else if ok {
		return existing, false, nil
	}
	if !coordownership.IsLiveCoordinator(coordinator, coordinator.ProjectID) {
		return domain.CoordinatorSummary{}, false, apierr.Conflict("SUMMARY_COORDINATOR_TERMINATED", "The coordinator is terminated", nil)
	}
	scope := strings.TrimSpace(in.Scope)
	if scope == "" {
		scope = coordinator.CoordinatorScope
	}
	now := s.now().UTC()
	rec := domain.CoordinatorSummary{
		ID: s.newID(), CoordinatorSessionID: coordinator.ID, ProjectID: coordinator.ProjectID,
		SubmissionKey: key, Scope: scope, State: in.State, WorkerSessionIDs: in.WorkerSessionIDs,
		Result: in.Result, EscalationReason: in.EscalationReason, EscalationNote: in.EscalationNote,
		CreatedAt: now, DeliveryState: domain.ReportPending,
	}
	if err := domain.ValidateCoordinatorSummary(rec); err != nil {
		return domain.CoordinatorSummary{}, false, apierr.Invalid("INVALID_SUMMARY", "Summary scope, state, workers, result, or escalation are invalid", nil)
	}
	for _, id := range rec.WorkerSessionIDs {
		worker, ok, err := s.store.GetSession(ctx, id)
		if err != nil {
			return domain.CoordinatorSummary{}, false, err
		}
		if !ok || worker.Kind != domain.KindWorker || worker.ProjectID != coordinator.ProjectID ||
			worker.OwnerSessionID == nil || *worker.OwnerSessionID != coordinator.ID {
			return domain.CoordinatorSummary{}, false, apierr.Invalid("SUMMARY_WORKER_NOT_OWNED", "Summary workers must be owned by the submitting coordinator", map[string]any{"workerSessionId": string(id)})
		}
	}
	// An escalation is available at once; everything else waits one batch window
	// so a chatty coordinator reaches the root as a single turn.
	rec.AvailableAt = now.Add(domain.CoordinatorSummaryBatchWindow)
	if rec.IsEscalation() {
		rec.AvailableAt = now
	}
	created, isNew, err := s.store.CreateCoordinatorSummary(ctx, rec)
	if err != nil {
		return domain.CoordinatorSummary{}, false, err
	}
	if isNew && s.onCreated != nil {
		s.onCreated(created)
	}
	return created, isNew, nil
}

// List returns one coordinator's summaries newest first, including delivery
// state, so operators can read the latest accepted summary and the history.
func (s *SummaryService) List(ctx context.Context, coordinator domain.SessionID, limit int) ([]domain.CoordinatorSummary, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("coordinator summary: store is required")
	}
	if limit <= 0 {
		limit = DefaultSummaryListLimit
	}
	if limit > MaxSummaryListLimit {
		limit = MaxSummaryListLimit
	}
	session, found, err := s.store.GetSession(ctx, coordinator)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
	}
	if session.Kind != domain.KindCoordinator {
		return nil, apierr.Invalid("SUMMARY_COORDINATOR_REQUIRED", "Summaries belong only to coordinator sessions", nil)
	}
	return s.store.ListCoordinatorSummaries(ctx, coordinator, int64(limit))
}
