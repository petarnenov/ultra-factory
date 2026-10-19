package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type fakeAccountTokens struct {
	tokens map[string]string
	errs   map[string]error
}

func (f fakeAccountTokens) TokenForAccount(_ context.Context, account string) (string, error) {
	if err := f.errs[account]; err != nil {
		return "", err
	}
	if token := f.tokens[account]; token != "" {
		return token, nil
	}
	return "", errors.New("no token")
}

func TestProviderUsesProjectAccountToken(t *testing.T) {
	var mu sync.Mutex
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth = append(auth, r.Header.Get("Authorization"))
		mu.Unlock()
		switch r.URL.Path {
		case "/repos/acme/demo/pulls":
			_, _ = w.Write([]byte("[]"))
		case "/graphql":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"pr0": map[string]any{"repository": map[string]any{"pullRequest": nil}}},
			})
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	provider, err := NewProvider(ProviderOptions{
		Client: NewClient(ClientOptions{
			Token:      StaticTokenSource("machine-token"),
			RESTBase:   srv.URL,
			GraphQLURL: srv.URL + "/graphql",
		}),
		Accounts: fakeAccountTokens{tokens: map[string]string{
			"account-a": "token-a",
			"account-b": "token-b",
		}},
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	repo := ports.SCMRepo{Provider: "github", Host: "github.com", Owner: "acme", Name: "demo", Account: "account-a"}
	if _, err := provider.RepoPRListGuard(context.Background(), repo, ""); err != nil {
		t.Fatalf("RepoPRListGuard: %v", err)
	}
	if _, err := provider.FetchPullRequests(context.Background(), []ports.SCMPRRef{
		{Repo: repo, Number: 1},
		{Repo: ports.SCMRepo{Provider: "github", Host: "github.com", Owner: "acme", Name: "demo", Account: "account-b"}, Number: 2},
	}); err != nil {
		t.Fatalf("FetchPullRequests: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"Bearer token-a", "Bearer token-a", "Bearer token-b"}
	if len(auth) != len(want) {
		t.Fatalf("auth calls = %v, want %v", auth, want)
	}
	for i := range want {
		if auth[i] != want[i] {
			t.Fatalf("auth calls = %v, want %v", auth, want)
		}
	}
}

func TestProviderRejectsMissingProjectAccountToken(t *testing.T) {
	provider, err := NewProvider(ProviderOptions{
		Client:   NewClient(ClientOptions{Token: StaticTokenSource("machine-token")}),
		Accounts: fakeAccountTokens{errs: map[string]error{"account-a": errors.New("missing")}},
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	repo := ports.SCMRepo{Provider: "github", Host: "github.com", Owner: "acme", Name: "demo", Account: "account-a"}
	_, err = provider.RepoPRListGuard(context.Background(), repo, "")
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("err = %v, want ErrAuthFailed", err)
	}
}

func TestAuthenticatedIdentityForAccountUsesBoundToken(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"login":"account-a","type":"User"}`))
	}))
	t.Cleanup(srv.Close)
	provider, err := NewProvider(ProviderOptions{
		Client:   NewClient(ClientOptions{Token: StaticTokenSource("machine-token"), RESTBase: srv.URL}),
		Accounts: fakeAccountTokens{tokens: map[string]string{"account-a": "token-a"}},
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	identity, err := provider.AuthenticatedIdentityForAccount(context.Background(), "account-a")
	if err != nil {
		t.Fatalf("AuthenticatedIdentityForAccount: %v", err)
	}
	if identity.Login != "account-a" || !identity.Human {
		t.Fatalf("identity = %+v", identity)
	}
	if auth != "Bearer token-a" {
		t.Fatalf("authorization = %q, want account token", auth)
	}
}
