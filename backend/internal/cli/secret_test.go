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
	body   []byte
}

func secretServer(t *testing.T, response string) (*httptest.Server, *secretCapture) {
	t.Helper()
	capture := &secretCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.method = r.Method
		capture.path = r.URL.Path
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
	const fakeToken = "ghp_FAKE444444444444444444444444444444"
	cfg := setConfigEnv(t)
	srv, capture := secretServer(t, `{"status":"ok","account":"work-account"}`)
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
	srv, capture := secretServer(t, `{"accounts":["hobby","work-account"]}`)
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
	if !strings.Contains(out, "github/hobby") || !strings.Contains(out, "github/work-account") {
		t.Fatalf("output = %q, want both logins", out)
	}
	if strings.Contains(out, "ghp_") {
		t.Fatal("list output contained a token")
	}
}

func TestSecretRemove(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := secretServer(t, `{"status":"ok","account":"work-account"}`)
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
