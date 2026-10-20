//go:build e2e && !windows

package cli_test

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

func seedPilotWorld(t *testing.T, e env, repo string) pilotWorld {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(e.dataDir)
	if err != nil {
		t.Fatalf("open seeded store: %v", err)
	}
	defer store.Close()
	now := time.Now().UTC().Truncate(time.Second)
	// Coordinators are off by default; the pilot opts in through set-config.
	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "ao", Path: repo, RegisteredAt: now}); err != nil {
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

// pilotRepo makes the project repository and puts a stand-in agent first on
// PATH, so a real `ao spawn` launches a session without a model behind it.
func pilotRepo(t *testing.T) string {
	t.Helper()
	repo, remote := t.TempDir(), t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "init"},
		{"init", "-q", "--bare", remote},
		{"remote", "add", "origin", remote},
		{"push", "-q", "-u", "origin", "main"},
		{"remote", "set-head", "origin", "main"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	bin := t.TempDir()
	script := "#!/bin/sh\nexec sleep 3600\n"
	for _, name := range []string{"claude"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return repo
}

// stopDaemon stops the daemon and waits until it no longer answers, so a
// later startDaemon is a real process restart over the same data dir.
func (e env) stopDaemon(t *testing.T) {
	t.Helper()
	e.run(t, "stop")
	deadline := time.Now().Add(10 * time.Second)
	for {
		out, _ := e.run(t, "status", "--json")
		if !strings.Contains(out, `"state": "ready"`) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon still ready 10s after stop:\n%s", out)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// recipientsByWorker maps each worker to the recipients its reports were
// claimed for; the claim records the owner it resolved, durably.
func recipientsByWorker(t *testing.T, e env) map[domain.SessionID]map[domain.SessionID]int {
	t.Helper()
	out := map[domain.SessionID]map[domain.SessionID]int{}
	for _, r := range pilotReports(t, e) {
		if r.DeliveryOwnerID == "" {
			continue
		}
		if out[r.SessionID] == nil {
			out[r.SessionID] = map[domain.SessionID]int{}
		}
		out[r.SessionID][domain.SessionID(r.DeliveryOwnerID)]++
	}
	return out
}

// waitRecipients waits for a claim by each expected recipient, and requires
// every claim to name the recipient expected for its worker.
func waitRecipients(t *testing.T, e env, want map[domain.SessionID]domain.SessionID) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		got := recipientsByWorker(t, e)
		seen := map[domain.SessionID]bool{}
		for worker, claimedFor := range got {
			for recipient := range claimedFor {
				if recipient != want[worker] {
					t.Fatalf("worker %s's report was claimed for %s, want %s (all claims: %v)", worker, recipient, want[worker], got)
				}
				seen[recipient] = true
			}
		}
		all := true
		for _, recipient := range want {
			all = all && seen[recipient]
		}
		if all {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("recipients = %v, want a claim for each of %v", got, want)
		}
		time.Sleep(100 * time.Millisecond)
	}
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
	parent := t
	repo := pilotRepo(t)
	e.startDaemon(t)
	w := seedPilotWorld(t, e, repo)
	root := string(w.root.ID)
	asRoot := []string{"--requested-by", root}
	assign := func(worker domain.SessionRecord, owner domain.SessionID) (string, int) {
		args := append([]string{"session", "assign", string(worker.ID), "--owner", string(owner)}, asRoot...)
		return e.run(t, args...)
	}

	spawnCoordinator := func(caller domain.SessionID, scope string) (string, int) {
		environ := append(e.environ(""), "AO_SESSION_ID="+string(caller))
		return e.runEnv(t, environ, "spawn", "--project", "ao", "--kind", "coordinator", "--scope", scope,
			"--name", "pilot", "--harness", "claude-code", "--skip-agent-check")
	}

	t.Run("a coordinator spawn is refused until the project enables coordinators", func(t *testing.T) {
		out, code := spawnCoordinator(w.root.ID, "auth area, replacement")
		if code != 1 {
			t.Fatalf("spawn with the gate off: exit %d, want 1: %s", code, out)
		}
		mustContain(t, out, "COORDINATORS_DISABLED")
		if out, code := e.run(t, "project", "set-config", "ao", "--coordinators"); code != 0 {
			t.Fatalf("enable coordinators: exit %d: %s", code, out)
		}
	})

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
		// Each report is claimed for the worker's owner, not the root.
		want := map[domain.SessionID]domain.SessionID{}
		for i, wk := range w.workers[:6] {
			want[wk.ID] = w.coordA.ID
			if i >= 3 {
				want[wk.ID] = w.coordB.ID
			}
		}
		waitRecipients(t, e, want)
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
		// The returned workers' reports now go to the root.
		returned := map[domain.SessionID]bool{w.workers[0].ID: true, w.workers[1].ID: true, w.workers[2].ID: true}
		deadline = time.Now().Add(20 * time.Second)
		for {
			for worker, claimedFor := range recipientsByWorker(t, e) {
				if returned[worker] && claimedFor[w.root.ID] > 0 {
					return
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("no report of the returned workers was claimed for the root: %v", recipientsByWorker(t, e))
			}
			time.Sleep(100 * time.Millisecond)
		}
	})

	t.Run("recovery: a replacement coordinator takes the returned workers", func(t *testing.T) {
		if _, err := exec.LookPath("tmux"); err != nil {
			t.Skip("a real spawn needs tmux on PATH")
		}
		out, code := spawnCoordinator(w.root.ID, "auth area, replacement")
		if code != 0 {
			t.Fatalf("spawn replacement coordinator: exit %d: %s", code, out)
		}
		m := regexp.MustCompile(`spawned session (\S+)`).FindStringSubmatch(out)
		if m == nil {
			t.Fatalf("spawn did not print a session id: %q", out)
		}
		w.coordC = domain.SessionRecord{ID: domain.SessionID(m[1])}
		// Sessions outlive the daemon; kill the spawned one while it is up.
		parent.Cleanup(func() { e.run(parent, "session", "kill", m[1]) })
		for _, wk := range w.workers[:3] {
			if out, code := assign(wk, w.coordC.ID); code != 0 {
				t.Fatalf("reassign %s: exit %d: %s", wk.ID, code, out)
			}
		}
		out, code = assign(w.workers[6], w.coordC.ID)
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

	t.Run("a daemon restart with a claimed, unacknowledged batch loses and duplicates nothing", func(t *testing.T) {
		if w.coordC.ID == "" {
			t.Skip("the replacement coordinator was not spawned")
		}
		e.stopDaemon(t)
		store, err := sqlite.Open(e.dataDir)
		if err != nil {
			t.Fatal(err)
		}
		// The daemon died after claiming a batch for the replacement coordinator
		// and before the turn was acknowledged.
		now := time.Now().UTC()
		claimed, err := store.ClaimPendingReportBatch(context.Background(), "ao", w.coordC.ID, w.root.ID, "claim-before-crash", now)
		before := pilotReportsFrom(t, store)
		_ = store.Close()
		if err != nil {
			t.Fatalf("claim before crash: %v", err)
		}
		if len(claimed) == 0 {
			t.Fatal("the replacement's workers had no report to claim")
		}
		// Started on the parent, so it outlives this subtest for the kill above.
		e.startDaemon(parent)
		parent.Cleanup(func() { e.run(parent, "session", "kill", string(w.coordC.ID)) })
		// Startup requeues the claim; the next pass claims it again for the
		// same recipient. Every report is still there exactly once.
		deadline := time.Now().Add(15 * time.Second)
		for {
			after := pilotReports(t, e)
			requeued := true
			for _, r := range after {
				if r.ClaimToken == "claim-before-crash" {
					requeued = false
				}
			}
			if requeued && len(after) == len(before) {
				ids := map[string]bool{}
				for _, r := range after {
					if ids[r.ID] {
						t.Fatalf("report %s was duplicated by the restart", r.ID)
					}
					ids[r.ID] = true
				}
				break
			}
			if time.Now().After(deadline) {
				out, _ := e.run(t, "session", "ls", "--project", "ao")
				t.Fatalf("claim survived the restart or reports changed: before=%d after=%d\n%s", len(before), len(after), out)
			}
			time.Sleep(100 * time.Millisecond)
		}
		recipients := recipientsByWorker(t, e)
		for _, r := range claimed {
			if got := recipients[r.SessionID]; len(got) != 1 || got[w.coordC.ID] == 0 {
				t.Fatalf("report of %s was claimed for %v after the restart, want only %s", r.SessionID, got, w.coordC.ID)
			}
		}
	})
}

func pilotReportsFrom(t *testing.T, store *sqlite.Store) []domain.ReportRecord {
	t.Helper()
	recs, err := store.ListReportsByProject(context.Background(), "ao")
	if err != nil {
		t.Fatalf("list reports: %v", err)
	}
	return recs
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
