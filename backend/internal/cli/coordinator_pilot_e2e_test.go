//go:build e2e && !windows

package cli_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

// pilotWorld seeds one root, two coordinators and seven workers as rows;
// the seventh worker must be refused. No agent process is started.
type pilotWorld struct {
	root, coordA, coordB, coordC domain.SessionRecord
	workers                      []domain.SessionRecord // w1..w7
}

func seedPilotWorld(t *testing.T, e env) pilotWorld {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(e.dataDir)
	if err != nil {
		t.Fatalf("open seeded store: %v", err)
	}
	defer store.Close()
	now := time.Now().UTC().Truncate(time.Second)
	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "ao", Path: "/tmp/ao-pilot", RegisteredAt: now}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	create := func(kind domain.SessionKind, scope string) domain.SessionRecord {
		t.Helper()
		rec, err := store.CreateSession(ctx, domain.SessionRecord{
			ProjectID: "ao", Kind: kind, CoordinatorScope: scope, Harness: domain.HarnessClaudeCode,
			Activity:  domain.Activity{State: domain.ActivityActive, LastActivityAt: now},
			CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			t.Fatalf("seed %s: %v", kind, err)
		}
		return rec
	}
	w := pilotWorld{
		root:   create(domain.KindOrchestrator, ""),
		coordA: create(domain.KindCoordinator, "auth area"),
		coordB: create(domain.KindCoordinator, "billing area"),
	}
	for i := 0; i < 7; i++ {
		w.workers = append(w.workers, create(domain.KindWorker, ""))
	}
	return w
}

// pilotReports reads the durable delivery state straight from the daemon's
// database; the reports API deliberately does not expose it.
func pilotReports(t *testing.T, e env) []domain.ReportRecord {
	t.Helper()
	store, err := sqlite.Open(e.dataDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	recs, err := store.ListReportsByProject(context.Background(), "ao")
	if err != nil {
		t.Fatalf("list reports: %v", err)
	}
	return recs
}

// TestE2E_CoordinatorPilot drives one root, two coordinators and six workers
// through the real binary; service/coordinatorpilot covers live delivery.
func TestE2E_CoordinatorPilot(t *testing.T) {
	e := newEnv(t)
	e.startDaemon(t)
	w := seedPilotWorld(t, e)
	root := string(w.root.ID)
	asRoot := []string{"--requested-by", root}
	assign := func(worker domain.SessionRecord, owner domain.SessionID) (string, int) {
		args := append([]string{"session", "assign", string(worker.ID), "--owner", string(owner)}, asRoot...)
		return e.run(t, args...)
	}

	t.Run("three workers per coordinator and capacity rejection for a fourth", func(t *testing.T) {
		for i, wk := range w.workers[:6] {
			owner := w.coordA.ID
			if i >= 3 {
				owner = w.coordB.ID
			}
			if out, code := assign(wk, owner); code != 0 {
				t.Fatalf("assign %s to %s: exit %d: %s", wk.ID, owner, code, out)
			}
		}
		r := e.roster(t, "ao", false)
		if len(r.workersOf(w.coordA.ID)) != 3 || len(r.workersOf(w.coordB.ID)) != 3 || len(r.RootWorkerSessionIDs) != 1 {
			t.Fatalf("roster = %+v, want 3 + 3 workers and one root-owned", r)
		}
		for _, owner := range []domain.SessionID{w.coordA.ID, w.coordB.ID} {
			out, code := assign(w.workers[6], owner)
			if code != 1 {
				t.Fatalf("fourth worker into %s: exit %d, want 1: %s", owner, code, out)
			}
			mustContain(t, out, "COORDINATOR_WORKER_LIMIT_REACHED")
			if requestIDPattern.FindStringSubmatch(out) == nil {
				t.Fatalf("capacity rejection lost its request id: %q", out)
			}
		}
		if owner := e.sessionViewOwner(t, w.workers[6].ID); owner != nil {
			t.Fatalf("rejected worker acquired owner %s", *owner)
		}
	})

	t.Run("a third coordinator and a coordinator-spawned coordinator are refused", func(t *testing.T) {
		spawn := func(caller domain.SessionID) (string, int) {
			environ := append(e.environ(""), "AO_SESSION_ID="+string(caller))
			return e.runEnv(t, environ, "spawn", "--project", "ao", "--kind", "coordinator", "--scope", "third area",
				"--name", "third", "--harness", "claude-code", "--skip-agent-check")
		}
		out, code := spawn(w.root.ID)
		if code != 1 {
			t.Fatalf("third coordinator: exit %d, want 1: %s", code, out)
		}
		mustContain(t, out, "COORDINATOR_LIMIT_REACHED")
		out, code = spawn(w.coordA.ID)
		if code != 1 {
			t.Fatalf("coordinator-spawned coordinator: exit %d, want 1: %s", code, out)
		}
		mustContain(t, out, "COORDINATOR_ROOT_REQUIRED")
		if got := len(e.roster(t, "ao", false).Coordinators); got != 2 {
			t.Fatalf("roster has %d coordinators after refused spawns, want 2", got)
		}
	})

	t.Run("only the root can merge", func(t *testing.T) {
		for _, caller := range []domain.SessionID{w.coordA.ID, w.workers[0].ID} {
			status, _, raw := e.httpJSON(t, "POST", "prs/1/merge", map[string]any{
				"prUrl": "https://github.com/acme/ao/pull/1", "expectedHeadSha": strings.Repeat("a", 40),
				"requestedBySessionId": string(caller),
			})
			if status != http.StatusForbidden || !strings.Contains(string(raw), "MERGE_ROOT_ONLY") {
				t.Fatalf("merge by %s: %d %s, want 403 MERGE_ROOT_ONLY", caller, status, raw)
			}
		}
		// The root passes the authority check; the merge then stops at the
		// untracked pull request, not at MERGE_ROOT_ONLY.
		status, _, raw := e.httpJSON(t, "POST", "prs/1/merge", map[string]any{
			"prUrl": "https://github.com/acme/ao/pull/1", "expectedHeadSha": strings.Repeat("a", 40),
			"requestedBySessionId": root,
		})
		if status == http.StatusForbidden || strings.Contains(string(raw), "MERGE_ROOT_ONLY") {
			t.Fatalf("merge by the root was refused for authority: %d %s", status, raw)
		}
	})

	t.Run("owner-routed reports stay durable and unacknowledged without a live recipient", func(t *testing.T) {
		for _, wk := range w.workers[:6] {
			environ := append(e.environ(""), "AO_SESSION_ID="+string(wk.ID))
			if out, code := e.runEnv(t, environ, "report", "--needs-input", "--note", "decision needed from "+string(wk.ID)); code != 0 {
				t.Fatalf("report from %s: exit %d: %s", wk.ID, code, out)
			}
		}
		waitReports(t, e, 6)
		// Give the delivery loop time to attempt and defer: a recipient with no
		// live agent must leave the reports pending, never acknowledge them.
		time.Sleep(3 * time.Second)
		bySession := map[domain.SessionID]int{}
		for _, r := range pilotReports(t, e) {
			bySession[r.SessionID]++
			if r.DeliveryState == domain.ReportAcknowledged {
				t.Fatalf("report %s was acknowledged although no recipient accepted a turn", r.ID)
			}
		}
		for _, wk := range w.workers[:6] {
			if bySession[wk.ID] != 1 {
				t.Fatalf("worker %s has %d reports, want exactly 1", wk.ID, bySession[wk.ID])
			}
		}
	})

	t.Run("terminating a coordinator returns its workers to the root", func(t *testing.T) {
		out, code := e.run(t, "session", "kill", string(w.coordA.ID))
		if code != 0 {
			t.Fatalf("kill coordinator: exit %d: %s", code, out)
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			r := e.roster(t, "ao", false)
			if len(r.RootWorkerSessionIDs) == 4 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("workers were not returned to the root: %+v", r)
			}
			time.Sleep(100 * time.Millisecond)
		}
		for _, wk := range w.workers[:3] {
			view := e.ownershipOf(t, wk.ID)
			if view.OwnerSessionID != nil || !ptrEq(view.RootSessionID, root) {
				t.Fatalf("worker %s ownership after handback = %+v", wk.ID, view)
			}
		}
		out, _ = e.run(t, "coordinator", "ls", "--project", "ao", "--include-terminated")
		mustContain(t, out, string(w.coordB.ID))
	})

	t.Run("recovery: a replacement coordinator takes the returned workers", func(t *testing.T) {
		ctx := context.Background()
		store, err := sqlite.Open(e.dataDir)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC().Truncate(time.Second)
		w.coordC, err = store.CreateSession(ctx, domain.SessionRecord{
			ProjectID: "ao", Kind: domain.KindCoordinator, CoordinatorScope: "auth area, replacement", Harness: domain.HarnessClaudeCode,
			Activity:  domain.Activity{State: domain.ActivityActive, LastActivityAt: now},
			CreatedAt: now, UpdatedAt: now,
		})
		_ = store.Close()
		if err != nil {
			t.Fatalf("seed replacement coordinator: %v", err)
		}
		for _, wk := range w.workers[:3] {
			if out, code := assign(wk, w.coordC.ID); code != 0 {
				t.Fatalf("reassign %s: exit %d: %s", wk.ID, code, out)
			}
		}
		out, code := assign(w.workers[6], w.coordC.ID)
		if code != 1 {
			t.Fatalf("fourth worker into the replacement: exit %d, want 1: %s", code, out)
		}
		mustContain(t, out, "COORDINATOR_WORKER_LIMIT_REACHED")
		// A coordinator cannot reassign workers.
		args := []string{"session", "assign", string(w.workers[0].ID), "--owner", string(w.coordB.ID), "--requested-by", string(w.coordC.ID)}
		out, code = e.run(t, args...)
		if code != 1 {
			t.Fatalf("assignment by a coordinator: exit %d, want 1: %s", code, out)
		}
		mustContain(t, out, "OWNERSHIP_ROOT_ONLY")
		r := e.roster(t, "ao", false)
		if len(r.workersOf(w.coordC.ID)) != 3 || len(r.workersOf(w.coordB.ID)) != 3 {
			t.Fatalf("roster after recovery = %+v", r)
		}
		out, _ = e.run(t, "session", "ls", "--project", "ao")
		mustContain(t, out, string(w.coordC.ID))
	})
}

// waitReports polls the durable report table until n reports exist, so the
// test does not depend on the daemon's outbox timing.
func waitReports(t *testing.T, e env, n int) []domain.ReportRecord {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		recs := pilotReports(t, e)
		if len(recs) >= n {
			return recs
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d reports became durable", len(recs), n)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
