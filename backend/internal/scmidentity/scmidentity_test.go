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
	tokens  map[string]string
	err     error
	lookups []string
}

func (f *fakeTokens) GitHubToken(_ context.Context, host, login string) (string, error) {
	f.lookups = append(f.lookups, host+"/"+login)
	if f.err != nil {
		return "", f.err
	}
	return f.tokens[host+"/"+login], nil
}

func projectWithAccount(account, origin string) domain.ProjectRecord {
	return domain.ProjectRecord{
		ID:            "widgets",
		RepoOriginURL: origin,
		Config: domain.ProjectConfig{
			SCM: domain.SCMConfig{GitHub: domain.GitHubSCMConfig{Account: account}},
		},
	}
}

func TestResolveNoBindingKeepsFallback(t *testing.T) {
	tokens := &fakeTokens{tokens: map[string]string{"github.com/work-account": fakeToken}}
	resolved, err := New(tokens, func() (string, error) { return "worker-1", nil }).Resolve(context.Background(), projectWithAccount("", "https://github.com/acme/repo"))
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

func TestResolveGitHubComInjectsGHtoken(t *testing.T) {
	tokens := &fakeTokens{tokens: map[string]string{"github.com/work-account": fakeToken}}
	resolved, err := New(tokens, func() (string, error) { return "worker-1", nil }).Resolve(context.Background(), projectWithAccount("work-account", "https://github.com/acme/repo.git"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Host != "github.com" || resolved.Account != "work-account" {
		t.Fatalf("resolved = %+v", resolved)
	}
	if got := resolved.Env["GH_TOKEN"]; got != fakeToken {
		t.Fatalf("GH_TOKEN = %q, want stored token", got)
	}
	if got := resolved.Env["GH_HOST"]; got != "" {
		t.Fatalf("GH_HOST = %q, want neutralized", got)
	}
	if len(tokens.lookups) != 1 || tokens.lookups[0] != "github.com/work-account" {
		t.Fatalf("lookups = %v", tokens.lookups)
	}
}

func TestEnvForHostEnterpriseServerInjectsEnterpriseVariables(t *testing.T) {
	env := EnvForHost("ghe.example.com", fakeToken)
	if got := env["GH_HOST"]; got != "ghe.example.com" {
		t.Fatalf("GH_HOST = %q", got)
	}
	if got := env["GH_ENTERPRISE_TOKEN"]; got != fakeToken {
		t.Fatalf("GH_ENTERPRISE_TOKEN = %q", got)
	}
	if got := env["GITHUB_ENTERPRISE_TOKEN"]; got != fakeToken {
		t.Fatalf("GITHUB_ENTERPRISE_TOKEN = %q", got)
	}
	if _, ok := env["GH_TOKEN"]; ok {
		t.Fatal("GHES environment must not set GH_TOKEN")
	}
}

// *.ghe.com tenancy hosts are not enterprise to gh, which reads GH_TOKEN.
func TestResolveTenancyHostInjectsGHtoken(t *testing.T) {
	tokens := &fakeTokens{tokens: map[string]string{"geowealth.ghe.com/work-account": fakeToken}}
	resolved, err := New(tokens, func() (string, error) { return "worker-1", nil }).Resolve(context.Background(), projectWithAccount("work-account", "https://geowealth.ghe.com/acme/repo.git"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Host != "geowealth.ghe.com" {
		t.Fatalf("host = %q", resolved.Host)
	}
	if got := resolved.Env["GH_HOST"]; got != "geowealth.ghe.com" {
		t.Fatalf("GH_HOST = %q", got)
	}
	if got := resolved.Env["GH_TOKEN"]; got != fakeToken {
		t.Fatalf("GH_TOKEN = %q, want the bound token", got)
	}
	for _, key := range []string{"GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"} {
		value, ok := resolved.Env[key]
		if !ok || value != "" {
			t.Fatalf("%s = %q (present=%t), want blank", key, value, ok)
		}
	}
}

func TestResolveMissingTokenIsStructuredAndNamesHost(t *testing.T) {
	tokens := &fakeTokens{err: errors.New("no token")}
	resolved, err := New(tokens, func() (string, error) { return "worker-1", nil }).Resolve(context.Background(), projectWithAccount("work-account", "https://geowealth.ghe.com/acme/repo.git"))
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
	if unavailable.ProjectID != "widgets" || unavailable.Host != "geowealth.ghe.com" || unavailable.Account != "work-account" || unavailable.Machine != "worker-1" {
		t.Fatalf("unavailable = %+v", unavailable)
	}
	message := err.Error()
	for _, want := range []string{`"widgets"`, `"work-account"`, `"geowealth.ghe.com"`, `"worker-1"`, "ao secret set github work-account --github-host geowealth.ghe.com"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q does not contain %q", message, want)
		}
	}
	if strings.Contains(message, fakeToken) {
		t.Fatal("error leaked a token")
	}
}

func TestResolveEmptyStoredTokenIsUnavailable(t *testing.T) {
	tokens := &fakeTokens{tokens: map[string]string{"github.com/work-account": "   "}}
	_, err := New(tokens, func() (string, error) { return "worker-1", nil }).Resolve(context.Background(), projectWithAccount("work-account", "https://github.com/acme/repo"))
	var unavailable *UnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("err = %v, want *UnavailableError", err)
	}
}

func TestGitHubHostDerivesFromOriginThenCanonical(t *testing.T) {
	cases := []struct {
		name     string
		project  domain.ProjectRecord
		wantHost string
		wantErr  bool
	}{
		{name: "origin", project: domain.ProjectRecord{RepoOriginURL: "https://geowealth.ghe.com/acme/repo.git"}, wantHost: "geowealth.ghe.com"},
		{name: "canonical fallback", project: domain.ProjectRecord{Config: domain.ProjectConfig{CanonicalRepoURL: "https://github.com/acme/repo"}}, wantHost: "github.com"},
		{name: "default", project: domain.ProjectRecord{}, wantHost: "github.com"},
		{name: "non-github origin", project: domain.ProjectRecord{RepoOriginURL: "https://gitlab.com/acme/repo.git"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GitHubHost(tc.project)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.wantHost {
				t.Fatalf("host = %q, want %q", got, tc.wantHost)
			}
		})
	}
}

func TestTokenForAccountUsesHost(t *testing.T) {
	tokens := &fakeTokens{tokens: map[string]string{"geowealth.ghe.com/work-account": fakeToken}}
	resolver := New(tokens, func() (string, error) { return "worker-1", nil })
	got, err := resolver.TokenForAccount(context.Background(), "geowealth.ghe.com", "work-account")
	if err != nil || got != fakeToken {
		t.Fatalf("TokenForAccount = (%q, %v)", got, err)
	}
}

func TestHostnameFailureFallsBackToThisMachine(t *testing.T) {
	tokens := &fakeTokens{err: errors.New("missing")}
	_, err := New(tokens, func() (string, error) { return "", errors.New("no hostname") }).Resolve(context.Background(), projectWithAccount("work-account", "https://github.com/acme/repo"))
	if !strings.Contains(err.Error(), "this machine") {
		t.Fatalf("message %q does not name a fallback machine", err)
	}
}
