package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/remoteworker"
)

// RemoteWorkerLinkService records remote workers whose reports this daemon pulls.
type RemoteWorkerLinkService interface {
	Register(context.Context, remoteworker.RegisterInput) (remoteworker.Registration, error)
}

// RemoteWorkerOwnershipService is the link ownership read and write surface.
// It is optional so existing link-creation fakes stay unchanged.
type RemoteWorkerOwnershipService interface {
	List(context.Context, domain.ProjectID) ([]domain.RemoteWorkerLink, error)
	AssignOwner(
		ctx context.Context,
		hostID string,
		remoteSessionID domain.SessionID,
		ownerID *domain.SessionID,
		requestedBy domain.SessionID,
	) (domain.RemoteWorkerLink, error)
}

// RemoteWorkerLinksController owns /remote-worker-links. The route makes this
// daemon contact a saved host, so the LAN listener never serves it.
type RemoteWorkerLinksController struct{ Svc RemoteWorkerLinkService }

// Register mounts the remote worker link routes on r.
func (c *RemoteWorkerLinksController) Register(r chi.Router) {
	r.Post("/remote-worker-links", c.create)
	r.Get("/remote-worker-links", c.list)
	r.Put("/remote-worker-links/{hostId}/{remoteSessionId}/owner", c.assignOwner)
}

func (c *RemoteWorkerLinksController) create(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "POST", "/api/v1/remote-worker-links")
		return
	}
	var req CreateRemoteWorkerLinkRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	registration, err := c.Svc.Register(r.Context(), remoteworker.RegisterInput{
		ParentSessionID: domain.SessionID(req.ParentSessionID), HostID: req.HostID,
		RemoteSessionID: domain.SessionID(req.RemoteSessionID), RemoteProjectID: domain.ProjectID(req.RemoteProjectID),
		DisplayName: req.DisplayName,
	})
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	resp := remoteWorkerLinkResponse(registration.Link)
	resp.PullBlocked = registration.PullBlocked
	envelope.WriteJSON(w, http.StatusCreated, resp)
}

func (c *RemoteWorkerLinksController) list(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.Svc.(RemoteWorkerOwnershipService)
	if !ok {
		apispec.NotImplemented(w, r, "GET", "/api/v1/remote-worker-links")
		return
	}
	project := domain.ProjectID(strings.TrimSpace(r.URL.Query().Get("project")))
	links, err := svc.List(r.Context(), project)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	resp := ListRemoteWorkerLinksResponse{Links: make([]RemoteWorkerLinkResponse, 0, len(links))}
	for _, link := range links {
		resp.Links = append(resp.Links, remoteWorkerLinkResponse(link))
	}
	envelope.WriteJSON(w, http.StatusOK, resp)
}

func (c *RemoteWorkerLinksController) assignOwner(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.Svc.(RemoteWorkerOwnershipService)
	if !ok {
		apispec.NotImplemented(w, r, "PUT", "/api/v1/remote-worker-links/{hostId}/{remoteSessionId}/owner")
		return
	}
	var req SessionOwnerRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	var ownerID *domain.SessionID
	if req.OwnerSessionID != nil {
		value := domain.SessionID(*req.OwnerSessionID)
		ownerID = &value
	}
	link, err := svc.AssignOwner(r.Context(),
		chi.URLParam(r, "hostId"),
		domain.SessionID(chi.URLParam(r, "remoteSessionId")),
		ownerID,
		domain.SessionID(req.RequestedBySessionID),
	)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, remoteWorkerLinkResponse(link))
}

func remoteWorkerLinkResponse(link domain.RemoteWorkerLink) RemoteWorkerLinkResponse {
	return RemoteWorkerLinkResponse{
		HostID: link.HostID, HostLabel: link.HostLabel,
		RemoteSessionID: string(link.RemoteSessionID), RemoteProjectID: string(link.RemoteProjectID),
		ProjectID: string(link.ProjectID), DisplayName: link.DisplayName,
		OwnerSessionID: sessionIDString(link.OwnerSessionID), CreatedAt: link.CreatedAt,
	}
}
