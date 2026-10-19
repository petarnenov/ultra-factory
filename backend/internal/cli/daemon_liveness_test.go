package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/daemonmeta"
	"github.com/aoagents/agent-orchestrator/backend/internal/runfile"
)

// These tests cover a caller in a private PID namespace (an agent sandbox):
// the run-file's PID looks dead to it, yet the daemon answers on its port.

const (
	sandboxDaemonPID      = 4242
	sandboxDaemonInstance = "instance-a"
)

// sandboxDaemon serves /healthz and /readyz as the daemon with pid and
// instance, and counts requests to /api/v1/projects.
func sandboxDaemon(t *testing.T, pid int, instance string) (*httptest.Server, *int) {
	t.Helper()
	projects := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/healthz", "/readyz":
			status := "ok"
			if r.URL.Path == "/readyz" {
				status = "ready"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": status, "service": daemonmeta.ServiceName, "pid": pid, "instanceId": instance,
			})
		case "/api/v1/projects":
			projects++
			_, _ = w.Write([]byte(`{"projects":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &projects
}

func writeSandboxRunFile(t *testing.T, path string, port int) {
	t.Helper()
	if err := runfile.Write(path, runfile.Info{
		PID: sandboxDaemonPID, InstanceID: sandboxDaemonInstance, Port: port, StartedAt: time.Unix(100, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
}

func pidInvisibleContext() *commandContext {
	return &commandContext{deps: Deps{
		ProcessAlive: func(int) bool { return false },
		Now:          func() time.Time { return time.Unix(200, 0).UTC() },
		Sleep:        func(time.Duration) {},
	}.withDefaults()}
}

func TestAPIRequestReachesDaemonWhosePIDIsInvisible(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, projects := sandboxDaemon(t, sandboxDaemonPID, sandboxDaemonInstance)
	writeSandboxRunFile(t, cfg.runFile, serverPort(t, srv.URL))

	c := pidInvisibleContext()
	for range 2 {
		if err := c.getJSON(context.Background(), "projects", nil); err != nil {
			t.Fatalf("getJSON: %v", err)
		}
	}
	if *projects != 2 {
		t.Fatalf("projects requests = %d, want 2", *projects)
	}
}

func TestAPIRequestRefusesAnotherDaemonOnTheRecordedPort(t *testing.T) {
	for name, tc := range map[string]struct {
		pid      int
		instance string
	}{
		"other pid":      {pid: 7, instance: sandboxDaemonInstance},
		"other instance": {pid: sandboxDaemonPID, instance: "instance-b"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := setConfigEnv(t)
			srv, projects := sandboxDaemon(t, tc.pid, tc.instance)
			writeSandboxRunFile(t, cfg.runFile, serverPort(t, srv.URL))

			err := pidInvisibleContext().getJSON(context.Background(), "projects", nil)
			if !errors.Is(err, errDaemonNotRunning) {
				t.Fatalf("err = %v, want errDaemonNotRunning", err)
			}
			if *projects != 0 {
				t.Fatalf("request reached a daemon that did not write the run-file")
			}
		})
	}
}

func TestAPIRequestReportsNotRunningWhenNothingAnswers(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := sandboxDaemon(t, sandboxDaemonPID, sandboxDaemonInstance)
	port := serverPort(t, srv.URL)
	srv.Close()
	writeSandboxRunFile(t, cfg.runFile, port)

	if err := pidInvisibleContext().getJSON(context.Background(), "projects", nil); !errors.Is(err, errDaemonNotRunning) {
		t.Fatalf("err = %v, want errDaemonNotRunning", err)
	}
}

func TestStatusIsReadyWhenDaemonPIDIsInvisible(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := sandboxDaemon(t, sandboxDaemonPID, sandboxDaemonInstance)
	writeSandboxRunFile(t, cfg.runFile, serverPort(t, srv.URL))

	st, err := pidInvisibleContext().inspectDaemon(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.State != stateReady {
		t.Fatalf("state = %q (%s), want ready", st.State, st.Error)
	}
}

// ao stop from a sandbox must not delete a live daemon's run-file.
func TestStopKeepsRunFileOfDaemonWhosePIDIsInvisible(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := sandboxDaemon(t, sandboxDaemonPID, sandboxDaemonInstance)
	writeSandboxRunFile(t, cfg.runFile, serverPort(t, srv.URL))

	c := pidInvisibleContext()
	st, err := c.inspectDaemon(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.State == stateStale {
		t.Fatalf("live daemon reported stale: %s", st.Error)
	}
	now := time.Unix(200, 0).UTC()
	c.deps.Now = func() time.Time { now = now.Add(time.Second); return now }
	if _, err := c.waitForStopped(context.Background(), sandboxDaemonPID, cfg.runFile, cfg.dataDir, time.Second); err == nil {
		t.Fatal("waitForStopped reported stopped while the daemon still answers")
	}
	if info, err := runfile.Read(cfg.runFile); err != nil || info == nil {
		t.Fatalf("run-file removed while its daemon still answers (info=%v, err=%v)", info, err)
	}
}

func TestStaleRunFileIsRemovedWhenNothingAnswers(t *testing.T) {
	cfg := setConfigEnv(t)
	userShell(t)
	srv, _ := sandboxDaemon(t, sandboxDaemonPID, sandboxDaemonInstance)
	port := serverPort(t, srv.URL)
	srv.Close()
	writeSandboxRunFile(t, cfg.runFile, port)

	st, err := pidInvisibleContext().stopDaemon(context.Background(), stopOptions{timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if st.State != stateStopped {
		t.Fatalf("state = %q, want stopped", st.State)
	}
	if info, _ := runfile.Read(filepath.Clean(cfg.runFile)); info != nil {
		t.Fatal("stale run-file was not removed")
	}
}

// Inside an agent session an unreachable daemon is most likely hidden by a
// sandbox; the message must not send the agent to `ao start`.
func TestUnreachableDaemonMessageInAgentSession(t *testing.T) {
	for name, tc := range map[string]struct {
		sessionID   string
		wantContain string
		wantAbsent  string
	}{
		"agent session": {sessionID: "ultra-factory-7", wantContain: "outside the sandbox", wantAbsent: "start it with `ao start`"},
		"user shell":    {sessionID: "", wantContain: "start it with `ao start`", wantAbsent: "outside the sandbox"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := setConfigEnv(t)
			t.Setenv("AO_SESSION_ID", tc.sessionID)
			srv, _ := sandboxDaemon(t, sandboxDaemonPID, sandboxDaemonInstance)
			port := serverPort(t, srv.URL)
			srv.Close()
			writeSandboxRunFile(t, cfg.runFile, port)

			err := pidInvisibleContext().getJSON(context.Background(), "projects", nil)
			if !errors.Is(err, errDaemonNotRunning) {
				t.Fatalf("err = %v, want errDaemonNotRunning", err)
			}
			if msg := err.Error(); !strings.Contains(msg, tc.wantContain) || strings.Contains(msg, tc.wantAbsent) {
				t.Fatalf("message = %q, want it to contain %q and not %q", msg, tc.wantContain, tc.wantAbsent)
			}
		})
	}
}
