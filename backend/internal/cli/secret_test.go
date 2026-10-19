package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type secretCapture struct {
	method string
	path   string
	query  string
	body   []byte
}

func secretServer(t *testing.T, response string) (*httptest.Server, *secretCapture) {
	t.Helper()
	capture := &secretCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.method = r.Method
		capture.path = r.URL.Path
		capture.query = r.URL.RawQuery
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		capture.body = data
		if !strings.HasPrefix(r.URL.Path, "/api/v1/secrets/github") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(srv.Close)
	return srv, capture
}

func TestSecretSetReadsTokenFromStdin(t *testing.T) {
	const fakeToken = "fake-test-token-value"
	cfg := setConfigEnv(t)
	srv, capture := secretServer(t, `{"status":"ok","host":"github.com","account":"work-account"}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{
		In:           strings.NewReader(fakeToken + "\n"),
		ProcessAlive: func(int) bool { return true },
	}, "secret", "set", "github", "work-account")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodPut || capture.path != "/api/v1/secrets/github/work-account" {
		t.Fatalf("request = %s %s, want PUT /api/v1/secrets/github/work-account", capture.method, capture.path)
	}
	var body githubSecretRequest
	if err := json.Unmarshal(capture.body, &body); err != nil {
		t.Fatalf("decode request: %v\nbody=%s", err, capture.body)
	}
	if body.Token != fakeToken {
		t.Fatalf("request token = %q, want stdin token", body.Token)
	}
	if strings.Contains(out, fakeToken) || strings.Contains(errOut, fakeToken) {
		t.Fatal("command output echoed the token")
	}
}

func TestSecretListShowsLoginsOnly(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := secretServer(t, `{"accounts":[{"host":"github.com","login":"hobby"},{"host":"ghe.example.com","login":"work-account"}]}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "secret", "ls")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodGet || capture.path != "/api/v1/secrets/github" {
		t.Fatalf("request = %s %s, want GET /api/v1/secrets/github", capture.method, capture.path)
	}
	if !strings.Contains(out, "github/github.com/hobby") || !strings.Contains(out, "github/ghe.example.com/work-account") {
		t.Fatalf("output = %q, want both host/logins", out)
	}
	if strings.Contains(out, "fake-test-token") {
		t.Fatal("list output contained a token")
	}
}

func TestSecretRemove(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := secretServer(t, `{"status":"ok","host":"github.com","account":"work-account"}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "secret", "rm", "github", "work-account")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodDelete || capture.path != "/api/v1/secrets/github/work-account" {
		t.Fatalf("request = %s %s, want DELETE /api/v1/secrets/github/work-account", capture.method, capture.path)
	}
}

func TestSecretSetRejectsUnsupportedProvider(t *testing.T) {
	_, _, err := executeCLI(t, Deps{}, "secret", "set", "gitlab", "work-account")
	if err == nil {
		t.Fatal("unsupported provider accepted")
	}
	if ExitCode(err) != 2 {
		t.Fatalf("exit code = %d, want 2", ExitCode(err))
	}
}

func TestSecretSetAndRemovePassGitHubHost(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := secretServer(t, `{"status":"ok","host":"ghe.example.com","account":"work-account"}`)
	writeRunFileFor(t, cfg, srv)

	if _, errOut, err := executeCLI(t, Deps{
		In:           strings.NewReader("fake-test-token-value\n"),
		ProcessAlive: func(int) bool { return true },
	}, "secret", "set", "github", "work-account", "--github-host", "ghe.example.com"); err != nil {
		t.Fatalf("set: unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.query != "host=ghe.example.com" {
		t.Fatalf("set query = %q, want host", capture.query)
	}
	if _, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "secret", "rm", "github", "work-account", "--github-host", "ghe.example.com"); err != nil {
		t.Fatalf("rm: unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.query != "host=ghe.example.com" {
		t.Fatalf("rm query = %q, want host", capture.query)
	}
}

// --host means a paired AO machine elsewhere in the CLI, so it must not be
// accepted here as a GitHub host.
func TestSecretRejectsHostFlagForGitHubHost(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := secretServer(t, `{"status":"ok","host":"ghe.example.com","account":"work-account"}`)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{
		In:           strings.NewReader("fake-test-token-value\n"),
		ProcessAlive: func(int) bool { return true },
	}, "secret", "set", "github", "work-account", "--host", "ghe.example.com")
	if err == nil {
		t.Fatal("--host was accepted as a GitHub host")
	}
	if capture.method != "" {
		t.Fatalf("request = %s %s, want none", capture.method, capture.path)
	}
}
