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
	envelope.WriteJSON(w, http.StatusOK, coordinatorRosterResponse(roster))
}

func sessionOwnershipResponse(view sessionsvc.Ownership) SessionOwnershipResponse {
	out := SessionOwnershipResponse{
		SessionID:        string(view.Session.ID),
		ProjectID:        string(view.Session.ProjectID),
		Kind:             string(view.Session.Kind),
		OwnerSessionID:   sessionIDString(view.Session.OwnerSessionID),
		CoordinatorScope: view.Session.CoordinatorScope,
		RootSessionID:    sessionIDString(view.RootSessionID),
	}
	if view.Owner != nil {
		out.OwnerKind = string(view.Owner.Kind)
		out.OwnerScope = view.Owner.CoordinatorScope
	}
	return out
}

func coordinatorRosterResponse(roster sessionsvc.CoordinatorRoster) CoordinatorRosterResponse {
	out := CoordinatorRosterResponse{
		ProjectID:            string(roster.ProjectID),
		RootSessionID:        sessionIDString(roster.RootSessionID),
		Coordinators:         make([]CoordinatorRosterEntry, 0, len(roster.Coordinators)),
		RootWorkerSessionIDs: make([]string, 0, len(roster.RootWorkers)),
	}
	for _, entry := range roster.Coordinators {
		workers := make([]string, 0, len(entry.Workers))
		for _, worker := range entry.Workers {
			workers = append(workers, string(worker.ID))
		}
		out.Coordinators = append(out.Coordinators, CoordinatorRosterEntry{
			CoordinatorSessionID: string(entry.Coordinator.ID),
			CoordinatorScope:     entry.Coordinator.CoordinatorScope,
			IsTerminated:         entry.Coordinator.IsTerminated,
			WorkerSessionIDs:     workers,
			WorkerCount:          len(workers),
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
