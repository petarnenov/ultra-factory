package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	sessionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/session"
)

// SessionOwnershipService is the optional durable ownership surface.
// A service that does not implement it answers 501.
type SessionOwnershipService interface {
	AssignOwner(
		ctx context.Context,
		workerID domain.SessionID,
		ownerID *domain.SessionID,
		requestedBy domain.SessionID,
	) (domain.SessionRecord, error)
	SetCoordinatorLimits(
		ctx context.Context,
		coordinatorID domain.SessionID,
		update sessionsvc.CoordinatorLimitsUpdate,
		caller sessionsvc.CoordinatorLimitsCaller,
	) (domain.SessionRecord, error)
	SetCoordinatorWorkerAgent(
		ctx context.Context,
		coordinatorID domain.SessionID,
		selection domain.AgentSelection,
		caller sessionsvc.CoordinatorAgentCaller,
	) (domain.SessionRecord, error)
	Ownership(ctx context.Context, id domain.SessionID) (sessionsvc.Ownership, error)
	CoordinatorRoster(
		ctx context.Context,
		projectID domain.ProjectID,
		includeTerminated bool,
	) (sessionsvc.CoordinatorRoster, error)
}

// getOwnership reads one session's owner, coordinator scope, and live root.
func (c *SessionsController) getOwnership(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.Svc.(SessionOwnershipService)
	if !ok {
		apispec.NotImplemented(w, r, "GET", "/api/v1/sessions/{sessionId}/ownership")
		return
	}
	id := domain.SessionID(chi.URLParam(r, "sessionId"))
	view, err := svc.Ownership(r.Context(), id)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, sessionOwnershipResponse(view))
}

// assignOwner assigns, reassigns, or clears one worker's owner. The service
// enforces the live-root caller and ADR 0010 invariants I2-I5.
func (c *SessionsController) assignOwner(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.Svc.(SessionOwnershipService)
	if !ok {
		apispec.NotImplemented(w, r, "PUT", "/api/v1/sessions/{sessionId}/owner")
		return
	}
	var req SessionOwnerRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	id := domain.SessionID(chi.URLParam(r, "sessionId"))
	var ownerID *domain.SessionID
	if req.OwnerSessionID != nil {
		value := domain.SessionID(*req.OwnerSessionID)
		ownerID = &value
	}
	if _, err := svc.AssignOwner(r.Context(), id, ownerID, domain.SessionID(req.RequestedBySessionID)); err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	sess, err := c.Svc.Get(r.Context(), id)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, SessionResponse{Session: sessionView(r, sess)})
}

// putCoordinatorChange decodes a coordinator-change body, applies it, and
// answers with the coordinator's ownership view.
func putCoordinatorChange[T any](
	c *SessionsController, w http.ResponseWriter, r *http.Request, route string,
	apply func(svc SessionOwnershipService, id domain.SessionID, req T) error,
) {
	svc, ok := c.Svc.(SessionOwnershipService)
	if !ok {
		apispec.NotImplemented(w, r, "PUT", route)
		return
	}
	var req T
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	id := domain.SessionID(chi.URLParam(r, "sessionId"))
	if err := apply(svc, id, req); err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	view, err := svc.Ownership(r.Context(), id)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, sessionOwnershipResponse(view))
}

// setCoordinatorLimits changes a live coordinator's worker limit. The service
// enforces the live-root or operator caller and the project ceiling.
func (c *SessionsController) setCoordinatorLimits(w http.ResponseWriter, r *http.Request) {
	putCoordinatorChange(c, w, r, "/api/v1/sessions/{sessionId}/coordinator-limits",
		func(svc SessionOwnershipService, id domain.SessionID, req CoordinatorLimitsRequest) error {
			caller := sessionsvc.CoordinatorLimitsCaller{
				RequestedBy:       domain.SessionID(req.RequestedBySessionID),
				OperatorInitiated: req.OperatorInitiated,
				Origin:            req.Origin,
			}
			update := sessionsvc.CoordinatorLimitsUpdate{WorkerLimit: req.WorkerLimit}
			if req.HostSlots != nil {
				slots := hostSlotsFromInput(*req.HostSlots)
				update.HostSlots = &slots
			}
			_, err := svc.SetCoordinatorLimits(r.Context(), id, update, caller)
			return err
		})
}

// setCoordinatorWorkerAgent changes a live coordinator's worker agent
// selection. The service enforces the caller rules and the project profiles.
func (c *SessionsController) setCoordinatorWorkerAgent(w http.ResponseWriter, r *http.Request) {
	putCoordinatorChange(c, w, r, "/api/v1/sessions/{sessionId}/coordinator-worker-agent",
		func(svc SessionOwnershipService, id domain.SessionID, req CoordinatorWorkerAgentRequest) error {
			caller := sessionsvc.CoordinatorAgentCaller{
				RequestedBy:       domain.SessionID(req.RequestedBySessionID),
				OperatorInitiated: req.OperatorInitiated,
				Origin:            req.Origin,
			}
			_, err := svc.SetCoordinatorWorkerAgent(r.Context(), id, req.WorkerAgent, caller)
			return err
		})
}

// listCoordinators reads a project's coordinator roster and owned workers.
func (c *SessionsController) listCoordinators(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.Svc.(SessionOwnershipService)
	if !ok {
		apispec.NotImplemented(w, r, "GET", "/api/v1/projects/{id}/coordinators")
		return
	}
	includeTerminated := false
	if raw := r.URL.Query().Get("includeTerminated"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_QUERY", "includeTerminated must be true or false", nil)
			return
		}
		includeTerminated = parsed
	}
	project := domain.ProjectID(chi.URLParam(r, "id"))
	roster, err := svc.CoordinatorRoster(r.Context(), project, includeTerminated)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	out := coordinatorRosterResponse(roster)
	if c.Summaries != nil {
		for i, entry := range roster.Coordinators {
			recs, err := c.Summaries.List(r.Context(), entry.Coordinator.ID, 1)
			if err != nil {
				envelope.WriteError(w, r, err)
				return
			}
			if len(recs) > 0 {
				latest := coordinatorSummaryResponse(recs[0])
				out.Coordinators[i].LatestSummary = &latest
			}
		}
	}
	envelope.WriteJSON(w, http.StatusOK, out)
}

func sessionOwnershipResponse(view sessionsvc.Ownership) SessionOwnershipResponse {
	out := SessionOwnershipResponse{
		SessionID:        string(view.Session.ID),
		ProjectID:        string(view.Session.ProjectID),
		Kind:             string(view.Session.Kind),
		OwnerSessionID:   sessionIDString(view.Session.OwnerSessionID),
		CoordinatorScope: view.Session.CoordinatorScope,
		RootSessionID:    sessionIDString(view.RootSessionID),
		Capacity:         capacityResponse(view.Capacity),
		AgentProfileID:   profileIDString(view.Session.AgentProfileID),
		WorkerAgent:      view.Session.CoordinatorWorkerAgent,
	}
	if view.Session.Kind == domain.KindCoordinator {
		limit, active := view.Capacity.WorkerLimit, view.Capacity.CoordinatorWorkers
		out.EffectiveWorkerLimit, out.ActiveWorkerCount = &limit, &active
		out.WorkerLimit = view.Session.CoordinatorWorkerLimit
		placement := view.Capacity.PlacementLimit
		out.EffectivePlacementLimit = &placement
		out.HostSlots = hostSlotUsageResponse(view.Capacity.HostSlots)
	}
	if view.Owner != nil {
		out.OwnerKind = string(view.Owner.Kind)
		out.OwnerScope = view.Owner.CoordinatorScope
	}
	return out
}

func capacityResponse(c sessionsvc.CapacityView) CoordinatorCapacity {
	return CoordinatorCapacity{
		MaxCoordinators: c.Limits.Coordinators, MaxWorkersPerCoordinator: c.Limits.WorkersPerCoordinator,
		MaxWorkers: c.Limits.Workers, ActiveCoordinators: c.ActiveCoordinators, ActiveWorkers: c.ActiveWorkers,
	}
}

func coordinatorRosterResponse(roster sessionsvc.CoordinatorRoster) CoordinatorRosterResponse {
	out := CoordinatorRosterResponse{
		ProjectID:            string(roster.ProjectID),
		RootSessionID:        sessionIDString(roster.RootSessionID),
		Capacity:             capacityResponse(roster.Capacity),
		Coordinators:         make([]CoordinatorRosterEntry, 0, len(roster.Coordinators)),
		RootWorkerSessionIDs: make([]string, 0, len(roster.RootWorkers)),
	}
	for _, entry := range roster.Coordinators {
		workers := make([]string, 0, len(entry.Workers))
		profiles := make([]CoordinatorRosterWorker, 0, len(entry.Workers))
		for _, worker := range entry.Workers {
			workers = append(workers, string(worker.ID))
			profiles = append(profiles, CoordinatorRosterWorker{SessionID: string(worker.ID), AgentProfileID: profileIDString(worker.AgentProfileID)})
		}
		out.Coordinators = append(out.Coordinators, CoordinatorRosterEntry{
			CoordinatorSessionID:    string(entry.Coordinator.ID),
			CoordinatorScope:        entry.Coordinator.CoordinatorScope,
			IsTerminated:            entry.Coordinator.IsTerminated,
			WorkerSessionIDs:        workers,
			WorkerCount:             len(workers),
			Workers:                 profiles,
			AgentProfileID:          profileIDString(entry.Coordinator.AgentProfileID),
			WorkerAgent:             entry.Coordinator.CoordinatorWorkerAgent,
			WorkerLimit:             entry.Coordinator.CoordinatorWorkerLimit,
			EffectiveWorkerLimit:    entry.WorkerLimit,
			ActiveWorkerCount:       entry.ActiveWorkers,
			EffectivePlacementLimit: entry.PlacementLimit,
			HostSlots:               hostSlotUsageResponse(entry.HostSlots),
			NeedsRecovery:           entry.NeedsRecovery,
		})
	}
	for _, worker := range roster.RootWorkers {
		out.RootWorkerSessionIDs = append(out.RootWorkerSessionIDs, string(worker.ID))
	}
	return out
}

// sessionIDString converts an optional durable id to its wire form.
func sessionIDString(id *domain.SessionID) *string {
	if id == nil {
		return nil
	}
	value := string(*id)
	return &value
}

// profileIDString converts an optional profile ID to its wire form.
func profileIDString(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

// hostSlotsFromInput converts wire entries; the service validates them.
func hostSlotsFromInput(in []HostSlotInput) []domain.HostSlot {
	out := make([]domain.HostSlot, len(in))
	for i, entry := range in {
		out[i] = domain.HostSlot{HostID: entry.HostID, Slots: entry.Slots}
	}
	return out
}

func hostSlotUsageResponse(in []sessionsvc.HostSlotUsage) []CoordinatorHostSlot {
	out := make([]CoordinatorHostSlot, len(in))
	for i, slot := range in {
		out[i] = CoordinatorHostSlot{HostID: slot.HostID, Slots: slot.Slots, Used: slot.Used}
	}
	return out
}
