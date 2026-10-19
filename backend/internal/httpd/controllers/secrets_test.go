package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/service/scmsecret"
)

type fakeSecretService struct {
	accounts []scmsecret.Account
	set      map[string]string
	deleted  []string
}

func (f *fakeSecretService) SetGitHubToken(_ context.Context, host, login, token string) error {
	if f.set == nil {
		f.set = map[string]string{}
	}
	f.set[host+"/"+login] = token
	return nil
}

func (f *fakeSecretService) ListGitHubAccounts(context.Context) ([]scmsecret.Account, error) {
	return append([]scmsecret.Account(nil), f.accounts...), nil
}

func (f *fakeSecretService) DeleteGitHubToken(_ context.Context, host, login string) error {
	f.deleted = append(f.deleted, host+"/"+login)
	return nil
}

func TestPutGitHubSecretNeverEchoesToken(t *testing.T) {
	const fakeToken = "fake-test-token-value"
	svc := &fakeSecretService{}
	router := chi.NewRouter()
	(&SecretsController{Svc: svc}).Register(router)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/secrets/github/work-account?host=ghe.example.com", strings.NewReader(`{"token":"`+fakeToken+`"}`))
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), fakeToken) {
		t.Fatal("response echoed the token")
	}
	if got := svc.set["ghe.example.com/work-account"]; got != fakeToken {
		t.Fatalf("stored token = %q, want request token", got)
	}
	var response GitHubSecretMutationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Account != "work-account" || response.Host != "ghe.example.com" || response.Status != "ok" {
		t.Fatalf("response = %+v", response)
	}
}

func TestPutGitHubSecretDefaultsToGitHubCom(t *testing.T) {
	svc := &fakeSecretService{}
	router := chi.NewRouter()
	(&SecretsController{Svc: svc}).Register(router)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/secrets/github/work-account", strings.NewReader(`{"token":"value"}`))
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if got := svc.set["github.com/work-account"]; got != "value" {
		t.Fatalf("stored token = %q", got)
	}
}

func TestListGitHubSecretsReturnsHostsAndLoginsOnly(t *testing.T) {
	svc := &fakeSecretService{accounts: []scmsecret.Account{
		{Host: "ghe.example.com", Login: "work-account"},
		{Host: "github.com", Login: "hobby"},
	}}
	router := chi.NewRouter()
	(&SecretsController{Svc: svc}).Register(router)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/secrets/github", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	var response GitHubSecretAccountsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Accounts) != 2 || response.Accounts[0].Host != "ghe.example.com" || response.Accounts[1].Login != "hobby" {
		t.Fatalf("accounts = %+v", response.Accounts)
	}
	if strings.Contains(recorder.Body.String(), "fake-test-token") {
		t.Fatal("list response contained a token-like value")
	}
}

func TestPutGitHubSecretRejectsInvalidHost(t *testing.T) {
	svc := &fakeSecretService{}
	router := chi.NewRouter()
	(&SecretsController{Svc: svc}).Register(router)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/secrets/github/work-account?host=https://github.com", strings.NewReader(`{"token":"x"}`))
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestPutGitHubSecretRejectsInvalidLogin(t *testing.T) {
	svc := &fakeSecretService{}
	router := chi.NewRouter()
	(&SecretsController{Svc: svc}).Register(router)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/secrets/github/bad_login", strings.NewReader(`{"token":"x"}`))
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestPutGitHubSecretRequiresToken(t *testing.T) {
	svc := &fakeSecretService{}
	router := chi.NewRouter()
	(&SecretsController{Svc: svc}).Register(router)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/secrets/github/work-account", strings.NewReader(`{}`))
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestDeleteGitHubSecretUsesHost(t *testing.T) {
	svc := &fakeSecretService{}
	router := chi.NewRouter()
	(&SecretsController{Svc: svc}).Register(router)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/secrets/github/work-account?host=ghe.example.com", nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if len(svc.deleted) != 1 || svc.deleted[0] != "ghe.example.com/work-account" {
		t.Fatalf("deleted = %v", svc.deleted)
	}
}
