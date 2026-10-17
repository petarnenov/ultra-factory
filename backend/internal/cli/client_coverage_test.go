package cli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestCLI_ClientCoverageDaemonUnavailableError(t *testing.T) {
	cause := errors.New("boom")
	err := daemonUnavailableError{message: "msg", cause: cause}
	if err.Error() != "msg" {
		t.Fatalf("Error() = %q, want msg", err.Error())
	}
	if !errors.Is(err, errDaemonUnavailable) {
		t.Fatal("errors.Is(errDaemonUnavailable) = false, want true")
	}
	var target daemonUnavailableError
	if !errors.As(err, &target) {
		t.Fatal("errors.As(daemonUnavailableError) = false")
	}
	if !errors.Is(err, cause) {
		t.Fatal("errors.Is(cause) = false, want true")
	}
}

func TestCLI_ClientCoverageDaemonResponseBodyReadError(t *testing.T) {
	errReader := &coverageErrReader{err: errors.New("read boom")}
	body := daemonResponseBody{ReadCloser: errReader}
	_, err := body.Read(make([]byte, 4))
	if err == nil {
		t.Fatal("Read() error = nil, want daemonUnavailableError")
	}
	var target daemonUnavailableError
	if !errors.As(err, &target) {
		t.Fatalf("err type = %T, want daemonUnavailableError", err)
	}
	if !strings.Contains(err.Error(), "read boom") {
		t.Fatalf("err = %q, want read boom", err.Error())
	}
}

func TestCLI_ClientCoverageAPIResponseErrorEmptyMessage(t *testing.T) {
	err := apiResponseError{StatusCode: 409}
	if got := err.Error(); got != "daemon returned HTTP 409" {
		t.Fatalf("Error() = %q, want %q", got, "daemon returned HTTP 409")
	}
}

func TestCLI_ClientCoverageDoJSONMarshalError(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := transportServer(t, http.StatusOK, `{}`)
	writeRunFileFor(t, cfg, srv)
	c := &commandContext{deps: Deps{ProcessAlive: func(int) bool { return true }, HTTPClient: &http.Client{}}}
	err := c.doJSON(context.Background(), http.MethodPost, "sessions", make(chan int), nil)
	if err == nil {
		t.Fatal("doJSON() error = nil, want JSON marshal error")
	}
	if !strings.Contains(err.Error(), "json: unsupported type") {
		t.Fatalf("err = %q, want JSON marshal error", err.Error())
	}
}

type coverageErrReader struct{ err error }

func (r *coverageErrReader) Read([]byte) (int, error) { return 0, r.err }
func (r *coverageErrReader) Close() error             { return nil }

func TestCLI_ClientCoverageConfigLoadError(t *testing.T) {
	t.Setenv("AO_PORT", "bad")
	c := &commandContext{deps: Deps{ProcessAlive: func(int) bool { return true }}}
	err := c.doJSON(context.Background(), http.MethodGet, "status", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid AO_PORT") {
		t.Fatalf("err = %v, want invalid AO_PORT", err)
	}
}

func TestCLI_ClientCoverageRunfileReadError(t *testing.T) {
	cfg := setConfigEnv(t)
	if err := os.WriteFile(cfg.runFile, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &commandContext{deps: Deps{ProcessAlive: func(int) bool { return true }, HTTPClient: &http.Client{}}}
	err := c.doJSON(context.Background(), http.MethodGet, "status", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "parse run-file") {
		t.Fatalf("err = %v, want runfile read error", err)
	}
}

func TestCLI_ClientCoverageNewRequestError(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := transportServer(t, http.StatusOK, `{}`)
	writeRunFileFor(t, cfg, srv)
	c := &commandContext{deps: Deps{ProcessAlive: func(int) bool { return true }, HTTPClient: &http.Client{}}}
	err := c.doJSON(context.Background(), "BAD METHOD", "status", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid method") {
		t.Fatalf("err = %v, want invalid method error", err)
	}
}

func TestCLI_ClientCoverageRemoteDoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, http.StatusOK, map[string]any{"hostId": "host-remote", "apiVersion": 1})
	}))
	c := &commandContext{
		hostSelector: "remote",
		remote:       &remoteTarget{label: "remote", baseURL: srv.URL, hostID: "host-remote", password: "pw"},
		deps:         Deps{HTTPClient: &http.Client{}},
	}
	srv.Close()
	var out struct{ OK bool }
	err := c.doJSON(context.Background(), http.MethodGet, "status", nil, &out)
	if err == nil || !strings.Contains(err.Error(), "call remote host") {
		t.Fatalf("err = %v, want remote call error", err)
	}
}
