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
	unlock := s.lockOrchestratorProject(worker.ProjectID)
	defer unlock()
	if worker, ok, err = s.store.GetSession(ctx, workerID); err != nil {
		return domain.SessionRecord{}, fmt.Errorf("load worker %s: %w", workerID, err)
	} else if !ok {
		return domain.SessionRecord{}, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
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
		if err := s.checkReassignCapacity(ctx, worker, *ownerID); err != nil {
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

// checkReassignCapacity rejects moving a live worker into a full coordinator.
// A terminated worker or the current owner needs no slot.
func (s *Service) checkReassignCapacity(ctx context.Context, worker domain.SessionRecord, owner domain.SessionID) error {
	if worker.IsTerminated || (worker.OwnerSessionID != nil && *worker.OwnerSessionID == owner) {
		return nil
	}
	counts, err := s.countCapacity(ctx, worker.ProjectID)
	if err != nil {
		return err
	}
	gate, err := coordownership.LoadConfig(ctx, s.store, worker.ProjectID)
	if err != nil {
		return err
	}
	return counts.CheckOwner(&owner, gate.Limits())
}

// Ownership is one session's durable ownership plus the project's live root.
// Owner is nil for a root-owned worker and for a root or coordinator itself.
type Ownership struct {
	Session       domain.Session
	Owner         *domain.Session
	RootSessionID *domain.SessionID
	// Capacity is the project's effective limits and live census. For a
	// coordinator, WorkerLimit and ActiveWorkers are its own.
	Capacity CapacityView
}

// CapacityView is a project's effective limits and its live census, read
// with the same counting the spawn paths enforce.
type CapacityView struct {
	Limits             domain.CoordinatorLimits
	ActiveCoordinators int
	ActiveWorkers      int
	// WorkerLimit and CoordinatorWorkers are set for a coordinator only.
	WorkerLimit        int
	CoordinatorWorkers int
}

// capacityView reads the project's limits and census. A coordinator also gets
// its effective worker limit and live count.
func (s *Service) capacityView(ctx context.Context, project domain.ProjectID, coordinator *domain.SessionID) (CapacityView, error) {
	if project == "" || s.store == nil {
		return CapacityView{}, nil
	}
	gate, err := coordownership.LoadConfig(ctx, s.store, project)
	if err != nil {
		return CapacityView{}, err
	}
	counts, err := s.countCapacity(ctx, project)
	if err != nil {
		return CapacityView{}, err
	}
	return capacityFrom(counts, gate.Limits(), coordinator), nil
}

func capacityFrom(counts coordownership.Counts, limits domain.CoordinatorLimits, coordinator *domain.SessionID) CapacityView {
	view := CapacityView{Limits: limits, ActiveCoordinators: counts.Coordinators, ActiveWorkers: counts.Workers}
	if coordinator != nil {
		view.WorkerLimit = counts.OwnerLimit(*coordinator, limits)
		view.CoordinatorWorkers = counts.ByOwner[*coordinator]
	}
	return view
}

// CoordinatorRosterEntry is one coordinator and the local workers it owns.
type CoordinatorRosterEntry struct {
	Coordinator domain.Session
	Workers     []domain.Session
	// NeedsRecovery is derived: the coordinator is terminated while a live
	// worker still names it as owner.
	NeedsRecovery bool
	// WorkerLimit is the effective limit and ActiveWorkers the count the
	// spawn paths enforce, including remote workers.
	WorkerLimit   int
	ActiveWorkers int
}

// CoordinatorRoster is a project's local ownership hierarchy. Remote worker
// links are listed separately; their owners use the same coordinator IDs.
type CoordinatorRoster struct {
	ProjectID     domain.ProjectID
	RootSessionID *domain.SessionID
	// Limits are the project's effective limits; Capacity is its live census.
	Limits       domain.CoordinatorLimits
	Capacity     CapacityView
	Coordinators []CoordinatorRosterEntry
	RootWorkers  []domain.Session
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
	var coordinator *domain.SessionID
	if sess.Kind == domain.KindCoordinator {
		coordinator = &sess.ID
	}
	if out.Capacity, err = s.capacityView(ctx, sess.ProjectID, coordinator); err != nil {
		return Ownership{}, err
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
	var counts coordownership.Counts
	if s.store != nil {
		if counts, err = s.countCapacity(ctx, project); err != nil {
			return CoordinatorRoster{}, err
		}
	}
	gate, err := coordownership.LoadConfig(ctx, s.store, project)
	if err != nil {
		return CoordinatorRoster{}, err
	}
	out.Limits = gate.Limits()
	out.Capacity = capacityFrom(counts, out.Limits, nil)
	for _, coordinator := range coordinators {
		workers := byOwner[coordinator.ID]
		recovery := coordinator.IsTerminated && hasLiveWorker(workers)
		if coordinator.IsTerminated && !includeTerminated && !recovery {
			continue
		}
		out.Coordinators = append(out.Coordinators, CoordinatorRosterEntry{
			Coordinator: coordinator, Workers: workers, NeedsRecovery: recovery,
			WorkerLimit:   counts.OwnerLimit(coordinator.ID, out.Limits),
			ActiveWorkers: counts.ByOwner[coordinator.ID],
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

// CoordinatorLimitsCaller names who changes a limit: the operator for the
// live root (Origin ui, cli, api), or the root itself (Origin cli, api).
type CoordinatorLimitsCaller struct {
	RequestedBy       domain.SessionID
	OperatorInitiated bool
	Origin            string
}

// SetCoordinatorWorkerLimit changes a live coordinator's limit under the
// spawn lock; nil or an unchanged value is a no-op.
func (s *Service) SetCoordinatorWorkerLimit(
	ctx context.Context,
	coordinatorID domain.SessionID,
	limit *int,
	caller CoordinatorLimitsCaller,
) (domain.SessionRecord, error) {
	if s.store == nil {
		return domain.SessionRecord{}, fmt.Errorf("set coordinator limit %s: store is required", coordinatorID)
	}
	origin, err := limitOrigin(caller)
	if err != nil {
		return domain.SessionRecord{}, err
	}
	updated, changed, err := s.applyCoordinatorWorkerLimit(ctx, coordinatorID, limit, caller, origin)
	if err != nil || !changed {
		return updated, err
	}
	msg := fmt.Sprintf("Your worker limit is now %d. Running workers keep running; new workers are admitted only below the limit.", *limit)
	if s.manager == nil {
		return updated, nil
	}
	if sendErr := s.manager.Send(ctx, coordinatorID, msg, nil); sendErr != nil && s.logger != nil {
		s.logger.Warn("coordinator limit message not delivered", "session", coordinatorID, "error", sendErr)
	}
	return updated, nil
}

func (s *Service) applyCoordinatorWorkerLimit(
	ctx context.Context,
	coordinatorID domain.SessionID,
	limit *int,
	caller CoordinatorLimitsCaller,
	origin string,
) (domain.SessionRecord, bool, error) {
	coordinator, ok, err := s.store.GetSession(ctx, coordinatorID)
	if err != nil {
		return domain.SessionRecord{}, false, fmt.Errorf("load coordinator %s: %w", coordinatorID, err)
	}
	if !ok {
		return domain.SessionRecord{}, false, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
	}
	if coordinator.Kind != domain.KindCoordinator {
		return domain.SessionRecord{}, false, apierr.Conflict("OWNER_NOT_COORDINATOR", "The session is not a coordinator", map[string]any{"sessionId": string(coordinator.ID)})
	}
	if coordinator.IsTerminated {
		return domain.SessionRecord{}, false, apierr.Conflict("OWNER_TERMINATED", "The coordinator is terminated", map[string]any{"sessionId": string(coordinator.ID)})
	}
	unlock := s.lockOrchestratorProject(coordinator.ProjectID)
	defer unlock()
	actor, err := s.limitActor(ctx, coordinator.ProjectID, caller)
	if err != nil {
		return domain.SessionRecord{}, false, err
	}
	if limit == nil {
		return coordinator, false, nil
	}
	gate, err := coordownership.LoadConfig(ctx, s.store, coordinator.ProjectID)
	if err != nil {
		return domain.SessionRecord{}, false, err
	}
	if ceiling := gate.Limits().WorkersPerCoordinator; *limit < 1 || *limit > ceiling {
		return domain.SessionRecord{}, false, coordownership.WorkerLimitInvalidError(*limit, ceiling)
	}
	if stored := coordinator.CoordinatorWorkerLimit; stored != nil && *stored == *limit {
		return coordinator, false, nil
	}
	event := domain.CoordinatorLimitEvent{ActorSessionID: actor, Origin: origin}
	if _, err := s.store.SetCoordinatorWorkerLimit(ctx, coordinator.ID, *limit, event, s.now()); err != nil {
		return domain.SessionRecord{}, false, err
	}
	updated, _, err := s.store.GetSession(ctx, coordinator.ID)
	return updated, true, err
}

// limitOrigin validates the marker and origin before any lookup.
func limitOrigin(caller CoordinatorLimitsCaller) (string, error) {
	if caller.OperatorInitiated {
		if caller.RequestedBy != "" {
			return "", apierr.Invalid("OPERATOR_MARKER_NOT_ALLOWED", "operatorInitiated cannot be combined with requestedBySessionId", nil)
		}
		switch caller.Origin {
		case "ui", "cli", "api":
			return caller.Origin, nil
		}
		return "", apierr.Invalid("COORDINATOR_LIMITS_ORIGIN_INVALID", "origin must be ui, cli or api when operatorInitiated is true", nil)
	}
	switch caller.Origin {
	case "":
		return "api", nil
	case "cli", "api":
		return caller.Origin, nil
	}
	return "", apierr.Invalid("COORDINATOR_LIMITS_ORIGIN_INVALID", "origin must be cli or api for a root change", nil)
}

// limitActor resolves who changes a limit: the live root itself, or the
// operator on behalf of the project's live root.
func (s *Service) limitActor(ctx context.Context, project domain.ProjectID, caller CoordinatorLimitsCaller) (domain.SessionID, error) {
	refuse := func(details map[string]any) error {
		return apierr.New(apierr.KindForbidden, "COORDINATOR_ROOT_REQUIRED",
			"Only the project root, or the operator on its behalf, may change coordinator limits", details)
	}
	if caller.OperatorInitiated {
		root, err := s.liveRootID(ctx, project)
		if err != nil {
			return "", err
		}
		if root == nil {
			return "", refuse(map[string]any{"projectId": string(project)})
		}
		return *root, nil
	}
	rec, found, err := s.store.GetSession(ctx, caller.RequestedBy)
	if err != nil {
		return "", fmt.Errorf("load limit caller %s: %w", caller.RequestedBy, err)
	}
	if !found || !coordownership.IsLiveRoot(rec, project) {
		return "", refuse(map[string]any{"sessionId": string(caller.RequestedBy)})
	}
	return rec.ID, nil
}
