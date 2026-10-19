package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

type fakeSecretService struct {
	logins []string
	set    map[string]string
}

func (f *fakeSecretService) SetGitHubToken(_ context.Context, login, token string) error {
	if f.set == nil {
		f.set = map[string]string{}
	}
	f.set[login] = token
	return nil
}

func (f *fakeSecretService) ListGitHubLogins(context.Context) ([]string, error) {
	return append([]string(nil), f.logins...), nil
}

func (f *fakeSecretService) DeleteGitHubToken(_ context.Context, login string) error {
	delete(f.set, login)
	return nil
}

func TestPutGitHubSecretNeverEchoesToken(t *testing.T) {
	const fakeToken = "ghp_FAKE333333333333333333333333333333"
	svc := &fakeSecretService{}
	router := chi.NewRouter()
	(&SecretsController{Svc: svc}).Register(router)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/secrets/github/work-account", strings.NewReader(`{"token":"`+fakeToken+`"}`))
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), fakeToken) {
		t.Fatal("response echoed the token")
	}
	if got := svc.set["work-account"]; got != fakeToken {
		t.Fatalf("stored token = %q, want request token", got)
	}
	var response GitHubSecretMutationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Account != "work-account" || response.Status != "ok" {
		t.Fatalf("response = %+v", response)
	}
}

func TestListGitHubSecretsReturnsLoginsOnly(t *testing.T) {
	svc := &fakeSecretService{logins: []string{"hobby", "work-account"}}
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
	if len(response.Accounts) != 2 || response.Accounts[0] != "hobby" || response.Accounts[1] != "work-account" {
		t.Fatalf("accounts = %v", response.Accounts)
	}
	if strings.Contains(recorder.Body.String(), "ghp_") {
		t.Fatal("list response contained a token-like value")
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
