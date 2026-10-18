package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/outbox"
)

func queuedEntries(t *testing.T, cfg testConfig) []outbox.Item {
	t.Helper()
	items, rejected, errs := outbox.Pending([]string{filepath.Join(cfg.dataDir, "outbox")})
	if len(rejected) != 0 || len(errs) != 0 {
		t.Fatalf("outbox rejected=%v errs=%v", rejected, errs)
	}
	return items
}

func TestReportIsQueuedWhenNoDaemonIsRunning(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "ao-7")

	_, errOut, err := executeCLI(t, Deps{}, "report", "tests pass")
	if err != nil {
		t.Fatalf("report must succeed by queueing, got %v", err)
	}
	if !strings.Contains(errOut, "queued") {
		t.Errorf("stderr = %q, want a queued notice", errOut)
	}
	items := queuedEntries(t, cfg)
	if len(items) != 1 {
		t.Fatalf("queued %d entries, want 1", len(items))
	}
	e := items[0].Entry
	var body reportAPIRequest
	if err := json.Unmarshal(e.Body, &body); err != nil {
		t.Fatal(err)
	}
	if e.Kind != outbox.KindReport || e.Path != "reports" || e.SessionID != "ao-7" || body.Message != "tests pass" || body.SessionID != "ao-7" {
		t.Fatalf("queued entry = %+v body=%+v", e, body)
	}
}

// A daemon that received the report and failed may already have applied it;
// queueing would deliver it twice.
func TestReportIsNotQueuedWhenTheDaemonAnswered(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "ao-7")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	if _, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "report", "tests pass"); err == nil {
		t.Fatal("report must fail when the daemon answered with an error")
	}
	if items := queuedEntries(t, cfg); len(items) != 0 {
		t.Fatalf("queued %d entries after a delivered request, want 0", len(items))
	}
}

func TestActivityHookIsQueuedWhenNoDaemonIsRunning(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "ao-7")

	_, errOut, err := executeCLI(t, Deps{In: strings.NewReader(`{"reason":"logout"}`)}, "hooks", "claude-code", "session-end")
	if err != nil {
		t.Fatalf("hooks must exit 0, got %v", err)
	}
	if errOut != "" {
		t.Errorf("queued hook must not reach the agent's stderr, got %q", errOut)
	}
	items := queuedEntries(t, cfg)
	if len(items) != 1 {
		t.Fatalf("queued %d entries, want 1", len(items))
	}
	e := items[0].Entry
	if e.Kind != outbox.KindActivity || e.Path != "sessions/ao-7/activity" || e.ObservedAt.IsZero() {
		t.Fatalf("queued entry = %+v", e)
	}
}
