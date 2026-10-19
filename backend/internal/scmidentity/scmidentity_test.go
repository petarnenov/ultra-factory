package scmidentity

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

const fakeToken = "fake-test-token-value"

type fakeTokens struct {
	token   string
	err     error
	lookups []string
}

func (f *fakeTokens) GitHubToken(_ context.Context, login string) (string, error) {
	f.lookups = append(f.lookups, login)
	return f.token, f.err
}

func projectWithAccount(account string) domain.ProjectRecord {
	return domain.ProjectRecord{
		ID: "widgets",
		Config: domain.ProjectConfig{
			SCM: domain.SCMConfig{GitHub: domain.GitHubSCMConfig{Account: account}},
		},
	}
}

func TestResolveNoBindingKeepsFallback(t *testing.T) {
	tokens := &fakeTokens{token: fakeToken}
	resolved, err := New(tokens, func() (string, error) { return "worker-1", nil }).Resolve(context.Background(), projectWithAccount(""))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(resolved.Env) != 0 || resolved.Account != "" {
		t.Fatalf("resolved = %+v, want empty", resolved)
	}
	if len(tokens.lookups) != 0 {
		t.Fatalf("lookups = %v, want none", tokens.lookups)
	}
}

func TestResolveBoundAccountInjectsGHtoken(t *testing.T) {
	tokens := &fakeTokens{token: fakeToken}
	resolved, err := New(tokens, func() (string, error) { return "worker-1", nil }).Resolve(context.Background(), projectWithAccount("work-account"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Account != "work-account" {
		t.Fatalf("account = %q, want work-account", resolved.Account)
	}
	if got := resolved.Env["GH_TOKEN"]; got != fakeToken {
		t.Fatalf("GH_TOKEN = %q, want stored token", got)
	}
	if len(tokens.lookups) != 1 || tokens.lookups[0] != "work-account" {
		t.Fatalf("lookups = %v, want [work-account]", tokens.lookups)
	}
}

func TestResolveMissingTokenIsStructuredAndActionable(t *testing.T) {
	tokens := &fakeTokens{err: errors.New("no token")}
	resolved, err := New(tokens, func() (string, error) { return "worker-1", nil }).Resolve(context.Background(), projectWithAccount("work-account"))
	if err == nil {
		t.Fatal("Resolve returned nil error")
	}
	if resolved.Env != nil {
		t.Fatalf("resolved env = %v, want nil", resolved.Env)
	}
	var unavailable *UnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("err = %T, want *UnavailableError", err)
	}
	if unavailable.ProjectID != "widgets" || unavailable.Account != "work-account" || unavailable.Machine != "worker-1" {
		t.Fatalf("unavailable = %+v", unavailable)
	}
	message := err.Error()
	for _, want := range []string{`"widgets"`, `"work-account"`, `"worker-1"`, "ao secret set github work-account"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q does not contain %q", message, want)
		}
	}
	if strings.Contains(message, fakeToken) {
		t.Fatal("error leaked a token")
	}
}

func TestResolveEmptyStoredTokenIsUnavailable(t *testing.T) {
	tokens := &fakeTokens{token: "   "}
	_, err := New(tokens, func() (string, error) { return "worker-1", nil }).Resolve(context.Background(), projectWithAccount("work-account"))
	var unavailable *UnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("err = %v, want *UnavailableError", err)
	}
}

func TestHostnameFailureFallsBackToThisMachine(t *testing.T) {
	tokens := &fakeTokens{err: errors.New("missing")}
	_, err := New(tokens, func() (string, error) { return "", errors.New("no hostname") }).Resolve(context.Background(), projectWithAccount("work-account"))
	if !strings.Contains(err.Error(), "this machine") {
		t.Fatalf("message %q does not name a fallback machine", err)
	}
}
