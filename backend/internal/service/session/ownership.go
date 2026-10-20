package session

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

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
	if err := counts.CheckOwner(&owner, gate.Limits()); err != nil {
		return err
	}
	// Session records are workers on this daemon; a paired host's workers are
	// remote links, checked by the remote worker service.
	return coordownership.CheckLocalWorker(&owner, worker.ProjectID, gate)
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
	// PlacementLimit and HostSlots are the coordinator's placement view.
	PlacementLimit int
	HostSlots      []HostSlotUsage
}

// HostSlotUsage is one host slot entry with the coordinator's live workers on
// that host. Used can exceed Slots after a live reduction.
type HostSlotUsage struct {
	HostID string
	Slots  int
	Used   int
}

// hostSlotUsage pairs a coordinator's stored slots with its link counts.
func hostSlotUsage(counts coordownership.Counts, coordinator domain.SessionID) []HostSlotUsage {
	slots := counts.HostSlots[coordinator]
	out := make([]HostSlotUsage, len(slots))
	for i, slot := range slots {
		out[i] = HostSlotUsage{HostID: slot.HostID, Slots: slot.Slots, Used: counts.ByOwnerHost[coordinator][slot.HostID]}
	}
	return out
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
		view.PlacementLimit = counts.EffectivePlacementLimit(*coordinator, limits)
		view.HostSlots = hostSlotUsage(counts, *coordinator)
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
	// PlacementLimit is WorkerLimit, or with host slots the smaller of it and
	// the slot total. HostSlots lists each slot with its live use.
	PlacementLimit int
	HostSlots      []HostSlotUsage
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
			WorkerLimit:    counts.OwnerLimit(coordinator.ID, out.Limits),
			ActiveWorkers:  counts.ByOwner[coordinator.ID],
			PlacementLimit: counts.EffectivePlacementLimit(coordinator.ID, out.Limits),
			HostSlots:      hostSlotUsage(counts, coordinator.ID),
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

// CoordinatorLimitsUpdate is a partial live change: a nil field keeps the
// stored value, and an empty HostSlots clears the slots. HostSlots entries
// may name a host by ID or label.
type CoordinatorLimitsUpdate struct {
	WorkerLimit *int
	HostSlots   *[]domain.HostSlot
}

// SetCoordinatorLimits changes a live coordinator's worker limit and host
// slots under the spawn lock. The resulting pair is validated as a whole; a
// change that leaves the stored pair as it is is a no-op.
func (s *Service) SetCoordinatorLimits(
	ctx context.Context,
	coordinatorID domain.SessionID,
	update CoordinatorLimitsUpdate,
	caller CoordinatorLimitsCaller,
) (domain.SessionRecord, error) {
	if s.store == nil {
		return domain.SessionRecord{}, fmt.Errorf("set coordinator limits %s: store is required", coordinatorID)
	}
	origin, err := limitOrigin(caller)
	if err != nil {
		return domain.SessionRecord{}, err
	}
	updated, note, err := s.applyCoordinatorLimits(ctx, coordinatorID, update, caller, origin)
	if err != nil || note == "" {
		return updated, err
	}
	if s.manager == nil {
		return updated, nil
	}
	if sendErr := s.manager.Send(ctx, coordinatorID, note, nil); sendErr != nil && s.logger != nil {
		s.logger.Warn("coordinator limit message not delivered", "session", coordinatorID, "error", sendErr)
	}
	return updated, nil
}

// applyCoordinatorLimits writes the change and returns the system message for
// the coordinator, empty when nothing changed.
func (s *Service) applyCoordinatorLimits(
	ctx context.Context,
	coordinatorID domain.SessionID,
	update CoordinatorLimitsUpdate,
	caller CoordinatorLimitsCaller,
	origin string,
) (domain.SessionRecord, string, error) {
	// The project is fixed per session; state is checked under the lock,
	// after the caller is authorized.
	project, err := s.limitProject(ctx, coordinatorID, caller)
	if err != nil {
		return domain.SessionRecord{}, "", err
	}
	unlock := s.lockOrchestratorProject(project)
	defer unlock()
	actor, err := s.limitActor(ctx, project, caller)
	if err != nil {
		return domain.SessionRecord{}, "", err
	}
	coordinator, err := s.loadLiveCoordinator(ctx, coordinatorID)
	if err != nil {
		return domain.SessionRecord{}, "", err
	}
	if update.WorkerLimit == nil && update.HostSlots == nil {
		return coordinator, "", nil
	}
	gate, err := coordownership.LoadConfig(ctx, s.store, coordinator.ProjectID)
	if err != nil {
		return domain.SessionRecord{}, "", err
	}
	ceiling := gate.Limits().WorkersPerCoordinator
	limit := coordinator.CoordinatorWorkerLimit
	if update.WorkerLimit != nil {
		if *update.WorkerLimit < 1 || *update.WorkerLimit > ceiling {
			return domain.SessionRecord{}, "", coordownership.WorkerLimitInvalidError(*update.WorkerLimit, ceiling)
		}
		limit = update.WorkerLimit
	}
	effective := ceiling
	if limit != nil && *limit < ceiling {
		effective = *limit
	}
	slots := coordinator.CoordinatorHostSlots
	if update.HostSlots != nil {
		if slots, err = coordownership.NormalizeHostSlots(ctx, s.hosts, *update.HostSlots, gate.AllowedHosts, effective); err != nil {
			return domain.SessionRecord{}, "", err
		}
	} else if total := slotTotal(slots); total > effective {
		return domain.SessionRecord{}, "", coordownership.HostSlotsInvalidError("hostSlots add up to more than the coordinator's workerLimit", &effective)
	}
	sameLimit := (limit == nil && coordinator.CoordinatorWorkerLimit == nil) ||
		(limit != nil && coordinator.CoordinatorWorkerLimit != nil && *limit == *coordinator.CoordinatorWorkerLimit)
	if sameLimit && coordownership.SameHostSlots(slots, coordinator.CoordinatorHostSlots) {
		return coordinator, "", nil
	}
	event := domain.CoordinatorLimitEvent{ActorSessionID: actor, Origin: origin}
	if err := s.store.SetCoordinatorLimits(ctx, coordinator.ID, limit, slots, event, s.now()); err != nil {
		return domain.SessionRecord{}, "", err
	}
	updated, _, err := s.store.GetSession(ctx, coordinator.ID)
	return updated, limitsMessage(effective, slots), err
}

func slotTotal(slots []domain.HostSlot) int {
	total := 0
	for _, slot := range slots {
		total += slot.Slots
	}
	return total
}

// limitsMessage tells the coordinator its new limit and host slots.
func limitsMessage(limit int, slots []domain.HostSlot) string {
	msg := fmt.Sprintf("Your worker limit is now %d.", limit)
	if len(slots) == 0 {
		msg += " You have no host slots."
	} else {
		parts := make([]string, len(slots))
		for i, slot := range slots {
			parts[i] = fmt.Sprintf("%s:%d", slot.HostID, slot.Slots)
		}
		msg += " Your host slots are " + strings.Join(parts, ", ") + "; workers run only on those hosts."
	}
	return msg + " Running workers keep running; new workers are admitted only below the limit."
}

// limitOrigin validates the marker and origin before any lookup.
func limitOrigin(caller CoordinatorLimitsCaller) (string, error) {
	if caller.OperatorInitiated {
		if caller.RequestedBy != "" {
			return "", apierr.New(apierr.KindForbidden, "OPERATOR_MARKER_NOT_ALLOWED",
				"operatorInitiated cannot be combined with requestedBySessionId",
				map[string]any{"sessionId": string(caller.RequestedBy)})
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

// loadLiveCoordinator reads a session and requires a live coordinator.
func (s *Service) loadLiveCoordinator(ctx context.Context, id domain.SessionID) (domain.SessionRecord, error) {
	rec, ok, err := s.store.GetSession(ctx, id)
	if err != nil {
		return domain.SessionRecord{}, fmt.Errorf("load coordinator %s: %w", id, err)
	}
	if !ok {
		return domain.SessionRecord{}, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
	}
	if rec.Kind != domain.KindCoordinator {
		return domain.SessionRecord{}, apierr.Conflict("OWNER_NOT_COORDINATOR", "The session is not a coordinator", map[string]any{"sessionId": string(rec.ID)})
	}
	if rec.IsTerminated {
		return domain.SessionRecord{}, apierr.Conflict("OWNER_TERMINATED", "The coordinator is terminated", map[string]any{"sessionId": string(rec.ID)})
	}
	return rec, nil
}

// limitProject finds the target's project. An unknown target is a 404 only
// for a live root; any other caller is refused first.
func (s *Service) limitProject(ctx context.Context, id domain.SessionID, caller CoordinatorLimitsCaller) (domain.ProjectID, error) {
	rec, found, err := s.store.GetSession(ctx, id)
	if err != nil {
		return "", fmt.Errorf("load coordinator %s: %w", id, err)
	}
	if found {
		return rec.ProjectID, nil
	}
	if !caller.OperatorInitiated {
		root, ok, err := s.store.GetSession(ctx, caller.RequestedBy)
		if err != nil {
			return "", fmt.Errorf("load limit caller %s: %w", caller.RequestedBy, err)
		}
		if !ok || !coordownership.IsLiveRoot(root, root.ProjectID) {
			return "", apierr.New(apierr.KindForbidden, "COORDINATOR_ROOT_REQUIRED",
				"Only the project root, or the operator on its behalf, may change coordinator limits",
				map[string]any{"sessionId": string(caller.RequestedBy)})
		}
	}
	return "", apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
}
