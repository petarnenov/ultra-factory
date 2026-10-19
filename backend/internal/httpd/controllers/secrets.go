package controllers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/scmsecret"
)

// SecretService is the controller-facing contract for machine-local SCM
// credentials. It exposes accounts and mutations, never token reads.
type SecretService interface {
	SetGitHubToken(ctx context.Context, host, login, token string) error
	ListGitHubAccounts(ctx context.Context) ([]scmsecret.Account, error)
	DeleteGitHubToken(ctx context.Context, host, login string) error
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
	login, host, ok := secretRequestTarget(w, r)
	if !ok {
		return
	}
	var body PutGitHubSecretRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Token == "" {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_REQUEST", "A non-empty 'token' field is required.", nil)
		return
	}
	if err := c.Svc.SetGitHubToken(r.Context(), host, login, body.Token); err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, GitHubSecretMutationResponse{Status: "ok", Host: host, Account: login})
}

func (c *SecretsController) listGitHub(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodGet, "/api/v1/secrets/github")
		return
	}
	accounts, err := c.Svc.ListGitHubAccounts(r.Context())
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	out := make([]GitHubSecretAccount, 0, len(accounts))
	for _, account := range accounts {
		out = append(out, GitHubSecretAccount{Host: account.Host, Login: account.Login})
	}
	envelope.WriteJSON(w, http.StatusOK, GitHubSecretAccountsResponse{Accounts: out})
}

func (c *SecretsController) deleteGitHub(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodDelete, "/api/v1/secrets/github/{login}")
		return
	}
	login, host, ok := secretRequestTarget(w, r)
	if !ok {
		return
	}
	if err := c.Svc.DeleteGitHubToken(r.Context(), host, login); err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, GitHubSecretMutationResponse{Status: "ok", Host: host, Account: login})
}

func secretRequestTarget(w http.ResponseWriter, r *http.Request) (login, host string, ok bool) {
	login = chi.URLParam(r, "login")
	if err := domain.ValidateGitHubAccount(login); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_GITHUB_ACCOUNT", err.Error(), nil)
		return "", "", false
	}
	host, err := domain.NormalizeGitHubHost(r.URL.Query().Get("host"))
	if err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_GITHUB_HOST", err.Error(), nil)
		return "", "", false
	}
	return login, host, true
}
