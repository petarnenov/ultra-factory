package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	"github.com/aoagents/agent-orchestrator/backend/internal/tailnet"
)

// TailnetDirectory is the daemon's view of AO hosts on the tailnet.
// tailnet.Discovery is the production implementation.
type TailnetDirectory interface {
	Snapshot() tailnet.Snapshot
	Refresh(ctx context.Context) tailnet.Snapshot
	SetHidden(hostID string, hidden bool) error
	SetLabel(hostID, label string) error
}

// TailnetController serves the loopback-only tailnet host directory.
type TailnetController struct {
	Dir TailnetDirectory
}

// Register mounts the tailnet routes on the supplied router.
func (c *TailnetController) Register(r chi.Router) {
	r.Get("/tailnet/hosts", c.list)
	r.Post("/tailnet/hosts/refresh", c.refresh)
	r.Patch("/tailnet/hosts/{hostId}", c.update)
}

func (c *TailnetController) list(w http.ResponseWriter, r *http.Request) {
	if c.Dir == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/tailnet/hosts")
		return
	}
	envelope.WriteJSON(w, http.StatusOK, tailnetHostsResponse(c.Dir.Snapshot()))
}

func (c *TailnetController) refresh(w http.ResponseWriter, r *http.Request) {
	if c.Dir == nil {
		apispec.NotImplemented(w, r, "POST", "/api/v1/tailnet/hosts/refresh")
		return
	}
	envelope.WriteJSON(w, http.StatusOK, tailnetHostsResponse(c.Dir.Refresh(r.Context())))
}

func (c *TailnetController) update(w http.ResponseWriter, r *http.Request) {
	if c.Dir == nil {
		apispec.NotImplemented(w, r, "PATCH", "/api/v1/tailnet/hosts/{hostId}")
		return
	}
	hostID := strings.TrimSpace(chi.URLParam(r, "hostId"))
	var body UpdateTailnetHostRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || hostID == "" || (body.Hidden == nil && body.Label == nil) {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "invalid_request", "TAILNET_HOST_UPDATE", "hostId and at least one of hidden or label are required", nil)
		return
	}
	if body.Hidden != nil {
		if err := c.Dir.SetHidden(hostID, *body.Hidden); err != nil {
			envelope.WriteAPIError(w, r, http.StatusInternalServerError, "internal", "TAILNET_HOST_UPDATE", err.Error(), nil)
			return
		}
	}
	if body.Label != nil {
		if err := c.Dir.SetLabel(hostID, strings.TrimSpace(*body.Label)); err != nil {
			envelope.WriteAPIError(w, r, http.StatusInternalServerError, "internal", "TAILNET_HOST_UPDATE", err.Error(), nil)
			return
		}
	}
	envelope.WriteJSON(w, http.StatusOK, tailnetHostsResponse(c.Dir.Snapshot()))
}

func tailnetHostsResponse(snap tailnet.Snapshot) TailnetHostsResponse {
	res := TailnetHostsResponse{
		Available: snap.Available, Error: snap.Error, SelfLogin: snap.SelfLogin,
		LocalHostIDs: append([]string{}, snap.LocalHostIDs...), Hosts: []TailnetHost{},
	}
	if !snap.UpdatedAt.IsZero() {
		res.UpdatedAt = snap.UpdatedAt.UTC().Format(time.RFC3339)
	}
	for _, host := range snap.Hosts {
		item := TailnetHost{
			HostID: host.HostID, Label: host.Label, Name: host.Name, DNSName: host.DNSName,
			Address: host.Address, Port: host.Port, URL: host.URL, Online: host.Online, Hidden: host.Hidden,
		}
		if !host.LastSeen.IsZero() {
			item.LastSeen = host.LastSeen.UTC().Format(time.RFC3339)
		}
		res.Hosts = append(res.Hosts, item)
	}
	return res
}
