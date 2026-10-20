package session

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	coordownership "github.com/aoagents/agent-orchestrator/backend/internal/service/ownership"
)

// AssignOwner sets or clears one worker's owner; nil means root-owned.
// This is the service boundary the ownership write route calls.
func (s *Service) AssignOwner(
	ctx context.Context,
	workerID domain.SessionID,
	ownerID *domain.SessionID,
	requestedBy domain.SessionID,
) (domain.SessionRecord, error) {
	if s.store == nil {
		return domain.SessionRecord{}, fmt.Errorf("assign owner %s: store is required", workerID)
	}
	worker, ok, err := s.store.GetSession(ctx, workerID)
	if err != nil {
		return domain.SessionRecord{}, fmt.Errorf("load worker %s: %w", workerID, err)
	}
	if !ok {
		return domain.SessionRecord{}, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
	}
	if err := coordownership.RequireWorkerTarget(worker); err != nil {
		return domain.SessionRecord{}, err
	}
	if worker.ProjectID == "" {
		return domain.SessionRecord{}, apierr.Invalid("OWNERSHIP_PROJECT_REQUIRED", "Standalone workers have no project ownership", map[string]any{"sessionId": string(worker.ID)})
	}
	caller, ok, err := s.store.GetSession(ctx, requestedBy)
	if err != nil {
		return domain.SessionRecord{}, fmt.Errorf("load ownership caller %s: %w", requestedBy, err)
	}
	if !ok {
		return domain.SessionRecord{}, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
	}
	if err := coordownership.RequireLiveRoot(caller, worker.ProjectID); err != nil {
		return domain.SessionRecord{}, err
	}
	if ownerID != nil {
		owner, ok, err := s.store.GetSession(ctx, *ownerID)
		if err != nil {
			return domain.SessionRecord{}, fmt.Errorf("load owner %s: %w", *ownerID, err)
		}
		if !ok {
			return domain.SessionRecord{}, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
		}
		if err := coordownership.RequireCoordinator(owner, worker.ProjectID); err != nil {
			return domain.SessionRecord{}, err
		}
	}
	record, _, err := s.store.SetSessionOwner(ctx, worker.ID, ownerID, domain.OwnershipExplicitAssignment, s.now(), false)
	if errors.Is(err, ports.ErrOwnerReassignmentInFlight) {
		return domain.SessionRecord{}, apierr.Conflict("OWNER_REASSIGNMENT_IN_FLIGHT", "A report of this worker is claimed for delivery", map[string]any{"sessionId": string(worker.ID)})
	}
	if err != nil {
		return domain.SessionRecord{}, err
	}
	return record, nil
}

// Ownership is one session's durable ownership plus the project's live root.
// Owner is nil for a root-owned worker and for a root or coordinator itself.
type Ownership struct {
	Session       domain.Session
	Owner         *domain.Session
	RootSessionID *domain.SessionID
}

// CoordinatorRosterEntry is one coordinator and the local workers it owns.
type CoordinatorRosterEntry struct {
	Coordinator domain.Session
	Workers     []domain.Session
	// NeedsRecovery is derived: the coordinator is terminated while a live
	// worker still names it as owner.
	NeedsRecovery bool
}

// CoordinatorRoster is a project's local ownership hierarchy. Remote worker
// links are listed separately; their owners use the same coordinator IDs.
type CoordinatorRoster struct {
	ProjectID     domain.ProjectID
	RootSessionID *domain.SessionID
	Coordinators  []CoordinatorRosterEntry
	RootWorkers   []domain.Session
}

// Ownership reads one session's owner, coordinator scope, and live root.
func (s *Service) Ownership(ctx context.Context, id domain.SessionID) (Ownership, error) {
	sess, err := s.Get(ctx, id)
	if err != nil {
		return Ownership{}, err
	}
	out := Ownership{Session: sess}
	out.RootSessionID, err = s.liveRootID(ctx, sess.ProjectID)
	if err != nil {
		return Ownership{}, err
	}
	if sess.OwnerSessionID != nil {
		owner, err := s.Get(ctx, *sess.OwnerSessionID)
		if err != nil {
			return Ownership{}, err
		}
		out.Owner = &owner
	}
	return out, nil
}

// CoordinatorRoster groups local workers by durable owner. A terminated
// coordinator with a live worker is listed even without includeTerminated.
func (s *Service) CoordinatorRoster(
	ctx context.Context,
	project domain.ProjectID,
	includeTerminated bool,
) (CoordinatorRoster, error) {
	if _, err := s.requireProject(ctx, project); err != nil {
		return CoordinatorRoster{}, err
	}
	sessions, err := s.List(ctx, ListFilter{ProjectID: project})
	if err != nil {
		return CoordinatorRoster{}, err
	}
	out := CoordinatorRoster{ProjectID: project}
	out.RootSessionID, err = s.liveRootID(ctx, project)
	if err != nil {
		return CoordinatorRoster{}, err
	}
	byOwner := make(map[domain.SessionID][]domain.Session)
	var coordinators []domain.Session
	for _, sess := range sessions {
		switch sess.Kind {
		case domain.KindCoordinator:
			coordinators = append(coordinators, sess)
		case domain.KindWorker:
			if !includeTerminated && sess.IsTerminated {
				continue
			}
			if sess.OwnerSessionID == nil {
				out.RootWorkers = append(out.RootWorkers, sess)
				continue
			}
			byOwner[*sess.OwnerSessionID] = append(byOwner[*sess.OwnerSessionID], sess)
		}
	}
	for _, coordinator := range coordinators {
		workers := byOwner[coordinator.ID]
		recovery := coordinator.IsTerminated && hasLiveWorker(workers)
		if coordinator.IsTerminated && !includeTerminated && !recovery {
			continue
		}
		out.Coordinators = append(out.Coordinators, CoordinatorRosterEntry{
			Coordinator: coordinator, Workers: workers, NeedsRecovery: recovery,
		})
	}
	sort.Slice(out.Coordinators, func(i, j int) bool {
		return out.Coordinators[i].Coordinator.ID < out.Coordinators[j].Coordinator.ID
	})
	sort.Slice(out.RootWorkers, func(i, j int) bool {
		return out.RootWorkers[i].ID < out.RootWorkers[j].ID
	})
	for i := range out.Coordinators {
		workers := out.Coordinators[i].Workers
		sort.Slice(workers, func(a, b int) bool { return workers[a].ID < workers[b].ID })
	}
	return out, nil
}

// liveRootID resolves the project's live root, or nil when it has none.
func (s *Service) liveRootID(ctx context.Context, project domain.ProjectID) (*domain.SessionID, error) {
	if project == "" {
		return nil, nil
	}
	active := true
	roots, err := s.List(ctx, ListFilter{ProjectID: project, Active: &active, OrchestratorOnly: true})
	if err != nil {
		return nil, err
	}
	if len(roots) == 0 {
		return nil, nil
	}
	id := newestSession(roots).ID
	return &id, nil
}

func hasLiveWorker(workers []domain.Session) bool {
	for _, worker := range workers {
		if !worker.IsTerminated {
			return true
		}
	}
	return false
}
