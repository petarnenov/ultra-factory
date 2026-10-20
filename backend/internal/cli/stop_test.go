package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/daemonmeta"
	"github.com/aoagents/agent-orchestrator/backend/internal/runfile"
)

// TestRefuseDaemonControlInAgentSession covers both directions of the guard: a
// user's own shell is unaffected, an agent session is refused.
func TestRefuseDaemonControlInAgentSession(t *testing.T) {
	t.Setenv("AO_SESSION_ID", "")
	if err := refuseDaemonControlInAgentSession("stop"); err != nil {
		t.Fatalf("user shell was refused: %v", err)
	}

	t.Setenv("AO_SESSION_ID", "ultra-factory-1")
	err := refuseDaemonControlInAgentSession("stop")
	if err == nil || !strings.Contains(err.Error(), "`ao stop` is refused inside an AO agent session") {
		t.Fatalf("agent session err = %v, want refusal", err)
	}
}

// TestStopRefusedInAgentSessionLeavesDaemonRunning proves an agent-run stop
// never reaches the daemon and leaves its run-file intact.
func TestStopRefusedInAgentSessionLeavesDaemonRunning(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "ultra-factory-1")
	if err := runfile.Write(cfg.runFile, runfile.Info{PID: 4242, Port: 3001, StartedAt: time.Unix(100, 0).UTC()}); err != nil {
		t.Fatal(err)
	}

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "stop", "--json")
	if err == nil || !strings.Contains(err.Error(), "refused inside an AO agent session") {
		t.Fatalf("err = %v, want agent-session refusal", err)
	}
	if got := ExitCode(err); got != 1 {
		t.Fatalf("ExitCode(%v) = %d, want 1", err, got)
	}
	info, err := runfile.Read(cfg.runFile)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil || info.PID != 4242 {
		t.Fatalf("run-file changed after refused stop: %#v", info)
	}
}

// TestStopSurfacesSupervisedRefusal proves the CLI reports the daemon's own
// refusal (a supervised host) instead of a bare HTTP status.
func TestStopSurfacesSupervisedRefusal(t *testing.T) {
	cfg := setConfigEnv(t)
	userShell(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			_, _ = fmt.Fprintf(w, `{"status":"ok","service":%q,"pid":%d}`, daemonmeta.ServiceName, os.Getpid())
		case "/readyz":
			_, _ = fmt.Fprintf(w, `{"status":"ready","service":%q,"pid":%d}`, daemonmeta.ServiceName, os.Getpid())
		case "/shutdown":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"status":"shutdown_refused","message":"this daemon runs under a host supervisor"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	if err := runfile.Write(cfg.runFile, runfile.Info{PID: os.Getpid(), Port: serverPort(t, srv.URL), StartedAt: time.Unix(100, 0).UTC()}); err != nil {
		t.Fatal(err)
	}

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(pid int) bool { return pid == os.Getpid() }}, "stop", "--json")
	if err == nil || !strings.Contains(err.Error(), "host supervisor") {
		t.Fatalf("err = %v, want the daemon's refusal message", err)
	}
	if got := ExitCode(err); got != 1 {
		t.Fatalf("ExitCode(%v) = %d, want 1", err, got)
	}
}

// TestWaitForStoppedKeepsRunFileFromConcurrentStart guards against deleting a
// fresh daemon's handshake: if a concurrent `ao start` replaces running.json
// with a new live PID while we are polling the PID we stopped, waitForStopped
// must report stopped but leave the new run-file intact.
func TestWaitForStoppedKeepsRunFileFromConcurrentStart(t *testing.T) {
	dir := t.TempDir()
	runFile := filepath.Join(dir, "running.json")

	const stoppedPID, newPID = 1111, 2222
	// running.json now belongs to a different, live daemon.
	if err := runfile.Write(runFile, runfile.Info{PID: newPID, Port: 3001, StartedAt: time.Unix(100, 0).UTC()}); err != nil {
		t.Fatal(err)
	}

	c := &commandContext{deps: Deps{
		ProcessAlive: func(pid int) bool { return pid == newPID }, // stoppedPID is dead
		Now:          func() time.Time { return time.Unix(200, 0).UTC() },
		Sleep:        func(time.Duration) {},
	}.withDefaults()}

	st, err := c.waitForStopped(context.Background(), stoppedPID, runFile, dir, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != stateStopped {
		t.Fatalf("state = %q, want stopped", st.State)
	}

	info, err := runfile.Read(runFile)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("new daemon's run-file was deleted by stop of a different PID")
		return
	}
	if info.PID != newPID {
		t.Fatalf("run-file PID = %d, want %d (new daemon)", info.PID, newPID)
	}
}

// TestWaitForStoppedRemovesOwnRunFile confirms the normal path still cleans up:
// when the dead PID owns the run-file, it is removed.
func TestWaitForStoppedRemovesOwnRunFile(t *testing.T) {
	dir := t.TempDir()
	runFile := filepath.Join(dir, "running.json")

	const stoppedPID = 1111
	if err := runfile.Write(runFile, runfile.Info{PID: stoppedPID, Port: 3001, StartedAt: time.Unix(100, 0).UTC()}); err != nil {
		t.Fatal(err)
	}

	c := &commandContext{deps: Deps{
		ProcessAlive: func(int) bool { return false },
		Now:          func() time.Time { return time.Unix(200, 0).UTC() },
		Sleep:        func(time.Duration) {},
	}.withDefaults()}

	st, err := c.waitForStopped(context.Background(), stoppedPID, runFile, dir, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != stateStopped {
		t.Fatalf("state = %q, want stopped", st.State)
	}
	info, err := runfile.Read(runFile)
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Fatalf("own run-file should have been removed, got %#v", info)
	}
}

func TestWaitForStoppedWaitsAfterRunFileRemovedUntilProcessExits(t *testing.T) {
	dir := t.TempDir()
	runFile := filepath.Join(dir, "running.json")

	const stoppedPID = 1111
	now := time.Unix(200, 0).UTC()
	aliveChecks := 0
	sleeps := 0
	c := &commandContext{deps: Deps{
		ProcessAlive: func(int) bool {
			aliveChecks++
			return aliveChecks < 3
		},
		Now: func() time.Time {
			return now
		},
		Sleep: func(d time.Duration) {
			sleeps++
			now = now.Add(d)
		},
	}.withDefaults()}

	st, err := c.waitForStopped(context.Background(), stoppedPID, runFile, dir, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != stateStopped {
		t.Fatalf("state = %q, want stopped", st.State)
	}
	if sleeps == 0 {
		t.Fatal("waitForStopped returned before waiting for process exit")
	}
	if aliveChecks < 3 {
		t.Fatalf("process checks = %d, want at least 3", aliveChecks)
	}
}

// TestWaitForStoppedReportsStoppedWhenRunFileGoneButProcessLingers covers
// issue #2214: once the daemon has removed its run-file (its liveness marker)
// the stop is committed, so if the process is still draining background workers
// past the timeout, waitForStopped must report stopped rather than erroring.
func TestWaitForStoppedReportsStoppedWhenRunFileGoneButProcessLingers(t *testing.T) {
	dir := t.TempDir()
	runFile := filepath.Join(dir, "running.json") // never written: run-file already gone

	const stoppedPID = 1111
	now := time.Unix(200, 0).UTC()
	c := &commandContext{deps: Deps{
		ProcessAlive: func(int) bool { return true }, // process never exits
		Now:          func() time.Time { return now },
		Sleep:        func(d time.Duration) { now = now.Add(d) },
	}.withDefaults()}

	st, err := c.waitForStopped(context.Background(), stoppedPID, runFile, dir, time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.State != stateStopped {
		t.Fatalf("state = %q, want stopped", st.State)
	}
}
