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
	"github.com/aoagents/agent-orchestrator/backend/internal/service/agentprofile"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/remoteworker"
)

// RemoteWorkerLinkService records remote workers whose reports this daemon pulls.
type RemoteWorkerLinkService interface {
	Register(context.Context, remoteworker.RegisterInput) (remoteworker.Registration, error)
}

// RemoteWorkerCapacityService is the optional pre-spawn capacity check.
type RemoteWorkerCapacityService interface {
	CheckCapacity(ctx context.Context, parentID domain.SessionID, hostID string) error
}

// RemoteWorkerAgentService is the optional agent selection check for a remote
// worker.
type RemoteWorkerAgentService interface {
	CheckAgent(context.Context, remoteworker.AgentCheck) (remoteworker.AgentCheckResult, error)
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

// RemoteWorkerSyncService runs one discovery pass over the saved hosts.
type RemoteWorkerSyncService interface {
	Sync(context.Context) ([]remoteworker.HostSync, error)
}

// RemoteWorkerLinksController owns /remote-worker-links. These routes make
// this daemon contact saved hosts, so the LAN listener never serves them.
type RemoteWorkerLinksController struct {
	Svc  RemoteWorkerLinkService
	Sync RemoteWorkerSyncService
}

// Register mounts the remote worker link routes on r.
func (c *RemoteWorkerLinksController) Register(r chi.Router) {
	r.Post("/remote-worker-links", c.create)
	r.Post("/remote-worker-links/check", c.check)
	r.Post("/remote-worker-links/sync", c.sync)
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

func (c *RemoteWorkerLinksController) sync(w http.ResponseWriter, r *http.Request) {
	if c.Sync == nil {
		apispec.NotImplemented(w, r, "POST", "/api/v1/remote-worker-links/sync")
		return
	}
	results, err := c.Sync.Sync(r.Context())
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	resp := RemoteWorkerSyncResponse{Hosts: make([]RemoteWorkerHostSync, 0, len(results))}
	for _, result := range results {
		resp.Hosts = append(resp.Hosts, RemoteWorkerHostSync{
			HostID: result.HostID, HostLabel: result.Label, Linked: result.Linked, Skipped: result.Skipped,
			Blocked: result.Blocked, Error: result.Error,
		})
	}
	envelope.WriteJSON(w, http.StatusOK, resp)
}

func (c *RemoteWorkerLinksController) check(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.Svc.(RemoteWorkerCapacityService)
	if !ok {
		apispec.NotImplemented(w, r, "POST", "/api/v1/remote-worker-links/check")
		return
	}
	var req CheckRemoteWorkerCapacityRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	if err := svc.CheckCapacity(r.Context(), domain.SessionID(req.ParentSessionID), req.HostID); err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	resp := CheckRemoteWorkerCapacityResponse{OK: true}
	if agents, ok := c.Svc.(RemoteWorkerAgentService); ok && req.AgentCheck && req.HostID != "" {
		check := remoteworker.AgentCheck{
			ParentSessionID: domain.SessionID(req.ParentSessionID), HostID: req.HostID,
			Explicit:         agentprofile.Explicit{Model: req.Model, Effort: req.Effort},
			ReadinessChecked: req.HostReadinessChecked, Unready: req.UnreadyHarnesses,
		}
		if req.HarnessExplicit {
			check.Explicit.Harness = domain.AgentHarness(req.Harness)
		}
		picked, err := agents.CheckAgent(r.Context(), check)
		if err != nil {
			envelope.WriteError(w, r, err)
			return
		}
		for _, h := range picked.NeedReadiness {
			resp.ReadinessHarnesses = append(resp.ReadinessHarnesses, string(h))
		}
		if p := picked.Profile; p != nil {
			resp.Agent = &RemoteWorkerAgent{ProfileID: p.ID, Harness: string(p.Harness), Model: p.Model, Effort: p.Effort}
		} else if picked.RecordID != "" {
			resp.Agent = &RemoteWorkerAgent{ProfileID: picked.RecordID, Harness: req.Harness, Model: req.Model, Effort: req.Effort}
		}
	}
	envelope.WriteJSON(w, http.StatusOK, resp)
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
		OwnerSessionID: sessionIDString(link.OwnerSessionID), Source: string(link.Source), CreatedAt: link.CreatedAt,
	}
}
