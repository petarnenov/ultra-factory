package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/scmsecret"
)

// SecretService is the controller-facing contract for machine-local SCM
// credentials. It exposes logins and mutations, never token reads.
type SecretService interface {
	SetGitHubToken(ctx context.Context, login, token string) error
	ListGitHubLogins(ctx context.Context) ([]string, error)
	DeleteGitHubToken(ctx context.Context, login string) error
}

// SecretsController owns the machine-local SCM secret routes. They are
// loopback-only: the LAN listener blocks /api/v1/secrets entirely.
type SecretsController struct {
	Svc SecretService
}

// Register mounts the secret routes on the supplied router.
func (c *SecretsController) Register(r chi.Router) {
	r.Put("/secrets/github/{login}", c.putGitHub)
	r.Get("/secrets/github", c.listGitHub)
	r.Delete("/secrets/github/{login}", c.deleteGitHub)
}

func (c *SecretsController) putGitHub(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodPut, "/api/v1/secrets/github/{login}")
		return
	}
	login := chi.URLParam(r, "login")
	if err := domain.ValidateGitHubAccount(login); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_GITHUB_ACCOUNT", err.Error(), nil)
		return
	}
	var body PutGitHubSecretRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Token == "" {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_REQUEST", "A non-empty 'token' field is required.", nil)
		return
	}
	if err := c.Svc.SetGitHubToken(r.Context(), login, body.Token); err != nil {
		if errors.Is(err, scmsecret.ErrInsecurePermissions) {
			envelope.WriteError(w, r, apierr.Internal("GITHUB_SECRET_STORE_FAILED", err.Error()))
			return
		}
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, GitHubSecretMutationResponse{Status: "ok", Account: login})
}

func (c *SecretsController) listGitHub(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodGet, "/api/v1/secrets/github")
		return
	}
	logins, err := c.Svc.ListGitHubLogins(r.Context())
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	if logins == nil {
		logins = []string{}
	}
	envelope.WriteJSON(w, http.StatusOK, GitHubSecretAccountsResponse{Accounts: logins})
}

func (c *SecretsController) deleteGitHub(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodDelete, "/api/v1/secrets/github/{login}")
		return
	}
	login := chi.URLParam(r, "login")
	if err := domain.ValidateGitHubAccount(login); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_GITHUB_ACCOUNT", err.Error(), nil)
		return
	}
	if err := c.Svc.DeleteGitHubToken(r.Context(), login); err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, GitHubSecretMutationResponse{Status: "ok", Account: login})
}
