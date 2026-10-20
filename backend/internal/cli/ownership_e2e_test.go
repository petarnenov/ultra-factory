//go:build e2e

package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

// ownershipWorld is the session set the scenarios share. Coordinators have
// no production path before CO-3, so rows are seeded into the daemon's DB.
type ownershipWorld struct {
	root, otherRoot                       domain.SessionRecord
	coordA, coordB, coordDead, coordOther domain.SessionRecord
	worker, worker2, standalone           domain.SessionRecord
}

func seedOwnershipWorld(t *testing.T, e env) ownershipWorld {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(e.dataDir)
	if err != nil {
		t.Fatalf("open seeded store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC().Truncate(time.Second)
	for _, id := range []string{"ao", "other"} {
		if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: id, Path: "/tmp/" + id, RegisteredAt: now,
			Config: domain.ProjectConfig{Coordinators: domain.CoordinatorsConfig{Enabled: true, AllowLocalWorkers: true}}}); err != nil {
			t.Fatalf("seed project %s: %v", id, err)
		}
	}
	create := func(project domain.ProjectID, kind domain.SessionKind, scope string) domain.SessionRecord {
		t.Helper()
		rec, err := store.CreateSession(ctx, domain.SessionRecord{
			ProjectID: project, Kind: kind, CoordinatorScope: scope, Harness: domain.HarnessClaudeCode,
			Activity:  domain.Activity{State: domain.ActivityActive, LastActivityAt: now},
			CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			t.Fatalf("seed %s session in %q: %v", kind, project, err)
		}
		return rec
	}
	w := ownershipWorld{
		root:       create("ao", domain.KindOrchestrator, ""),
		otherRoot:  create("other", domain.KindOrchestrator, ""),
		coordA:     create("ao", domain.KindCoordinator, "auth area"),
		coordB:     create("ao", domain.KindCoordinator, "billing area"),
		coordDead:  create("ao", domain.KindCoordinator, "retired area"),
		coordOther: create("other", domain.KindCoordinator, "foreign area"),
		worker:     create("ao", domain.KindWorker, ""),
		worker2:    create("ao", domain.KindWorker, ""),
		standalone: create("", domain.KindWorker, ""),
	}
	w.coordDead.IsTerminated = true
	w.coordDead.UpdatedAt = now.Add(time.Second)
	if err := store.UpdateSession(ctx, w.coordDead); err != nil {
		t.Fatalf("terminate seeded coordinator: %v", err)
	}
	return w
}

var requestIDPattern = regexp.MustCompile(`\[request ([^\]\s]+)\]`)

// httpJSON calls the disposable daemon and returns the status and raw body.
func (e env) httpJSON(t *testing.T, method, path string, body any) (int, http.Header, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d/api/v1/%s", e.port, path), reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s /api/v1/%s %v\n=> %d %s", method, path, body, res.StatusCode, strings.TrimSpace(string(raw)))
	return res.StatusCode, res.Header, raw
}

type ownershipView struct {
	SessionID        string  `json:"sessionId"`
	Kind             string  `json:"kind"`
	OwnerSessionID   *string `json:"ownerSessionId"`
	OwnerKind        string  `json:"ownerKind"`
	OwnerScope       string  `json:"ownerScope"`
	CoordinatorScope string  `json:"coordinatorScope"`
	RootSessionID    *string `json:"rootSessionId"`
}

func (e env) ownershipOf(t *testing.T, id domain.SessionID) ownershipView {
	t.Helper()
	status, _, raw := e.httpJSON(t, "GET", "sessions/"+string(id)+"/ownership", nil)
	if status != http.StatusOK {
		t.Fatalf("ownership of %s: status %d: %s", id, status, raw)
	}
	var view ownershipView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	return view
}

// sessionViewOwner reads the owner from GET /sessions/{id}.
func (e env) sessionViewOwner(t *testing.T, id domain.SessionID) *string {
	t.Helper()
	status, _, raw := e.httpJSON(t, "GET", "sessions/"+string(id), nil)
	if status != http.StatusOK {
		t.Fatalf("session %s: status %d: %s", id, status, raw)
	}
	var res struct {
		Session struct {
			OwnerSessionID *string `json:"ownerSessionId"`
		} `json:"session"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	return res.Session.OwnerSessionID
}

type rosterView struct {
	RootSessionID *string `json:"rootSessionId"`
	Coordinators  []struct {
		CoordinatorSessionID string   `json:"coordinatorSessionId"`
		CoordinatorScope     string   `json:"coordinatorScope"`
		IsTerminated         bool     `json:"isTerminated"`
		WorkerSessionIDs     []string `json:"workerSessionIds"`
		WorkerCount          int      `json:"workerCount"`
	} `json:"coordinators"`
	RootWorkerSessionIDs []string `json:"rootWorkerSessionIds"`
}

func (e env) roster(t *testing.T, project string, includeTerminated bool) rosterView {
	t.Helper()
	path := "projects/" + project + "/coordinators"
	if includeTerminated {
		path += "?includeTerminated=true"
	}
	status, _, raw := e.httpJSON(t, "GET", path, nil)
	if status != http.StatusOK {
		t.Fatalf("roster %s: status %d: %s", project, status, raw)
	}
	var view rosterView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	return view
}

func (r rosterView) workersOf(coordinator domain.SessionID) []string {
	for _, c := range r.Coordinators {
		if c.CoordinatorSessionID == string(coordinator) {
			return c.WorkerSessionIDs
		}
	}
	return nil
}

func contains(ids []string, want domain.SessionID) bool {
	for _, id := range ids {
		if id == string(want) {
			return true
		}
	}
	return false
}

func ptrEq(got *string, want string) bool { return got != nil && *got == want }

// TestE2E_Ownership drives the ADR 0010 ownership surface through the real
// ao binary and a disposable daemon.
func TestE2E_Ownership(t *testing.T) {
	e := newEnv(t)
	e.startDaemon(t)
	w := seedOwnershipWorld(t, e)
	root := string(w.root.ID)

	t.Run("root fallback before any assignment", func(t *testing.T) {
		view := e.ownershipOf(t, w.worker.ID)
		if view.OwnerSessionID != nil || !ptrEq(view.RootSessionID, root) || view.Kind != "worker" {
			t.Fatalf("unowned worker ownership = %+v, want nil owner and root %s", view, root)
		}
		if owner := e.sessionViewOwner(t, w.worker.ID); owner != nil {
			t.Fatalf("session view owner = %v, want nil", *owner)
		}
		r := e.roster(t, "ao", false)
		if !ptrEq(r.RootSessionID, root) || !contains(r.RootWorkerSessionIDs, w.worker.ID) || !contains(r.RootWorkerSessionIDs, w.worker2.ID) {
			t.Fatalf("roster = %+v, want unowned workers under root %s", r, root)
		}
		out, code := e.run(t, "session", "owner", string(w.worker.ID))
		if code != 0 {
			t.Fatalf("session owner exit %d: %s", code, out)
		}
		mustContain(t, out, "owner: root")
		mustContain(t, out, "root: "+root)
		out, _ = e.run(t, "session", "ls", "--project", "ao")
		mustContain(t, out, string(w.worker.ID))
		mustContain(t, out, "root")
	})

	t.Run("assign reads back through every surface", func(t *testing.T) {
		out, code := e.run(t, "session", "assign", string(w.worker.ID), "--owner", string(w.coordA.ID), "--requested-by", root)
		if code != 0 {
			t.Fatalf("assign exit %d: %s", code, out)
		}
		mustContain(t, out, "owner: "+string(w.coordA.ID))

		view := e.ownershipOf(t, w.worker.ID)
		if !ptrEq(view.OwnerSessionID, string(w.coordA.ID)) || view.OwnerKind != "coordinator" || view.OwnerScope != "auth area" {
			t.Fatalf("ownership after assign = %+v", view)
		}
		if owner := e.sessionViewOwner(t, w.worker.ID); !ptrEq(owner, string(w.coordA.ID)) {
			t.Fatalf("session view owner = %v, want %s", owner, w.coordA.ID)
		}
		r := e.roster(t, "ao", false)
		if got := r.workersOf(w.coordA.ID); !contains(got, w.worker.ID) {
			t.Fatalf("roster workers of %s = %v, want %s", w.coordA.ID, got, w.worker.ID)
		}
		if contains(r.RootWorkerSessionIDs, w.worker.ID) {
			t.Fatalf("assigned worker still listed as root-owned: %v", r.RootWorkerSessionIDs)
		}
		out, _ = e.run(t, "session", "owner", string(w.worker.ID))
		mustContain(t, out, "owner: "+string(w.coordA.ID)+" (coordinator, scope: auth area)")
		out, _ = e.run(t, "coordinator", "ls", "--project", "ao")
		mustContain(t, out, "coordinator "+string(w.coordA.ID)+" (scope: auth area)  workers: 1")
		out, _ = e.run(t, "session", "get", string(w.worker.ID))
		mustContain(t, out, string(w.coordA.ID))
	})

	t.Run("assign is idempotent", func(t *testing.T) {
		if out, code := e.run(t, "session", "assign", string(w.worker.ID), "--owner", string(w.coordA.ID), "--requested-by", root); code != 0 {
			t.Fatalf("repeat assign exit %d: %s", code, out)
		}
		if got := e.roster(t, "ao", false).workersOf(w.coordA.ID); len(got) != 1 {
			t.Fatalf("workers after repeat = %v, want exactly one", got)
		}
	})

	t.Run("reassign moves the worker", func(t *testing.T) {
		if out, code := e.run(t, "session", "assign", string(w.worker.ID), "--owner", string(w.coordB.ID), "--requested-by", root); code != 0 {
			t.Fatalf("reassign exit %d: %s", code, out)
		}
		if owner := e.sessionViewOwner(t, w.worker.ID); !ptrEq(owner, string(w.coordB.ID)) {
			t.Fatalf("session view owner = %v, want %s", owner, w.coordB.ID)
		}
		r := e.roster(t, "ao", false)
		if len(r.workersOf(w.coordA.ID)) != 0 || !contains(r.workersOf(w.coordB.ID), w.worker.ID) {
			t.Fatalf("roster after reassign = %+v", r)
		}
	})

	t.Run("AO_SESSION_ID supplies the caller by default", func(t *testing.T) {
		environ := append(e.environ(""), "AO_SESSION_ID="+root)
		out, code := e.runEnv(t, environ, "session", "assign", string(w.worker2.ID), "--owner", string(w.coordA.ID))
		if code != 0 {
			t.Fatalf("assign via AO_SESSION_ID exit %d: %s", code, out)
		}
		if owner := e.sessionViewOwner(t, w.worker2.ID); !ptrEq(owner, string(w.coordA.ID)) {
			t.Fatalf("worker2 owner = %v", owner)
		}
	})

	t.Run("clear returns the worker to root", func(t *testing.T) {
		out, code := e.run(t, "session", "assign", string(w.worker.ID), "--owner", "root", "--requested-by", root)
		if code != 0 {
			t.Fatalf("clear exit %d: %s", code, out)
		}
		mustContain(t, out, "owner: root")
		view := e.ownershipOf(t, w.worker.ID)
		if view.OwnerSessionID != nil || view.OwnerKind != "" || !ptrEq(view.RootSessionID, root) {
			t.Fatalf("ownership after clear = %+v", view)
		}
		if !contains(e.roster(t, "ao", false).RootWorkerSessionIDs, w.worker.ID) {
			t.Fatal("cleared worker missing from the root-owned list")
		}
		if owner := e.sessionViewOwner(t, w.worker.ID); owner != nil {
			t.Fatalf("session view owner = %v, want nil", *owner)
		}
		out, _ = e.run(t, "session", "owner", string(w.worker.ID))
		mustContain(t, out, "owner: root")
	})

	t.Run("coordinators and roots expose no owner", func(t *testing.T) {
		coord := e.ownershipOf(t, w.coordA.ID)
		if coord.OwnerSessionID != nil || coord.Kind != "coordinator" || coord.CoordinatorScope != "auth area" || !ptrEq(coord.RootSessionID, root) {
			t.Fatalf("coordinator ownership = %+v", coord)
		}
		rootView := e.ownershipOf(t, w.root.ID)
		if rootView.OwnerSessionID != nil || rootView.Kind != "orchestrator" {
			t.Fatalf("root ownership = %+v", rootView)
		}
	})

	t.Run("rejections carry the ADR error codes", func(t *testing.T) {
		cases := []struct {
			name        string
			worker      domain.SessionID
			owner       *string
			requestedBy domain.SessionID
			status      int
			code        string
		}{
			{"a root is never owned", w.root.ID, sp(w.coordA.ID), w.root.ID, http.StatusBadRequest, "OWNER_TARGET_NOT_WORKER"},
			{"a coordinator is never owned", w.coordB.ID, sp(w.coordA.ID), w.root.ID, http.StatusBadRequest, "OWNER_TARGET_NOT_WORKER"},
			{"a standalone worker has no project ownership", w.standalone.ID, sp(w.coordA.ID), w.root.ID, http.StatusBadRequest, "OWNERSHIP_PROJECT_REQUIRED"},
			{"a coordinator cannot write ownership", w.worker.ID, sp(w.coordA.ID), w.coordA.ID, http.StatusForbidden, "OWNERSHIP_ROOT_ONLY"},
			{"a worker cannot write ownership", w.worker.ID, sp(w.coordA.ID), w.worker2.ID, http.StatusForbidden, "OWNERSHIP_ROOT_ONLY"},
			{"another project's root cannot write ownership", w.worker.ID, sp(w.coordA.ID), w.otherRoot.ID, http.StatusForbidden, "OWNERSHIP_ROOT_ONLY"},
			{"the owner must be a coordinator: worker", w.worker.ID, sp(w.worker2.ID), w.root.ID, http.StatusConflict, "OWNER_NOT_COORDINATOR"},
			{"the owner must be a coordinator: root", w.worker.ID, sp(w.root.ID), w.root.ID, http.StatusConflict, "OWNER_NOT_COORDINATOR"},
			{"the owner must be in the same project", w.worker.ID, sp(w.coordOther.ID), w.root.ID, http.StatusConflict, "OWNER_CROSS_PROJECT"},
			{"the owner must be live", w.worker.ID, sp(w.coordDead.ID), w.root.ID, http.StatusConflict, "OWNER_TERMINATED"},
			{"unknown worker", "nope-1", sp(w.coordA.ID), w.root.ID, http.StatusNotFound, "SESSION_NOT_FOUND"},
			{"unknown owner", w.worker.ID, sp("nope-2"), w.root.ID, http.StatusNotFound, "SESSION_NOT_FOUND"},
			{"unknown caller", w.worker.ID, sp(w.coordA.ID), "nope-3", http.StatusNotFound, "SESSION_NOT_FOUND"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				body := map[string]any{"ownerSessionId": tc.owner, "requestedBySessionId": string(tc.requestedBy)}
				status, header, raw := e.httpJSON(t, "PUT", "sessions/"+string(tc.worker)+"/owner", body)
				var env struct {
					Code      string `json:"code"`
					RequestID string `json:"requestId"`
				}
				if err := json.Unmarshal(raw, &env); err != nil {
					t.Fatalf("decode error envelope: %v: %s", err, raw)
				}
				if status != tc.status || env.Code != tc.code {
					t.Fatalf("got %d %q, want %d %q: %s", status, env.Code, tc.status, tc.code, raw)
				}
				if env.RequestID == "" {
					t.Fatalf("error envelope has no requestId: %s", raw)
				}
				if got := header.Get("X-Request-Id"); got != "" && got != env.RequestID {
					t.Fatalf("X-Request-Id %q != envelope requestId %q", got, env.RequestID)
				}
			})
		}

		// None of the rejected writes may have changed anything.
		if view := e.ownershipOf(t, w.worker.ID); view.OwnerSessionID != nil {
			t.Fatalf("rejected writes changed the owner: %+v", view)
		}
		if view := e.ownershipOf(t, w.root.ID); view.OwnerSessionID != nil {
			t.Fatalf("root acquired an owner: %+v", view)
		}
	})

	t.Run("malformed bodies are rejected", func(t *testing.T) {
		status, _, raw := e.httpJSON(t, "PUT", "sessions/"+string(w.worker.ID)+"/owner",
			map[string]any{"ownerSessionId": nil, "requestedBySessionId": root, "extra": true})
		if status != http.StatusBadRequest || !strings.Contains(string(raw), "INVALID_JSON") {
			t.Fatalf("unknown field: %d %s", status, raw)
		}
		status, _, raw = e.httpJSON(t, "GET", "projects/ao/coordinators?includeTerminated=maybe", nil)
		if status != http.StatusBadRequest || !strings.Contains(string(raw), "INVALID_QUERY") {
			t.Fatalf("bad includeTerminated: %d %s", status, raw)
		}
	})

	t.Run("a worker with a claimed report cannot be reassigned", func(t *testing.T) {
		ctx := context.Background()
		store, err := sqlite.Open(e.dataDir)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		now := time.Now().UTC().Truncate(time.Second)
		if _, err := store.CreateReport(ctx, domain.ReportRecord{
			ID: "rpt-inflight", SessionID: w.worker2.ID, ProjectID: "ao", State: domain.ReportNeedsInput, Note: "x",
			CreatedAt: now, DeliveryState: domain.ReportPending, AvailableAt: now, RepeatCount: 1,
		}); err != nil {
			t.Fatal(err)
		}
		if _, claimed, err := store.ClaimReport(ctx, "rpt-inflight", "token", now); err != nil || !claimed {
			t.Fatalf("claim report: claimed=%v err=%v", claimed, err)
		}
		status, _, raw := e.httpJSON(t, "PUT", "sessions/"+string(w.worker2.ID)+"/owner",
			map[string]any{"ownerSessionId": string(w.coordB.ID), "requestedBySessionId": root})
		if status != http.StatusConflict || !strings.Contains(string(raw), "OWNER_REASSIGNMENT_IN_FLIGHT") {
			t.Fatalf("in-flight reassignment: %d %s", status, raw)
		}
		if owner := e.sessionViewOwner(t, w.worker2.ID); !ptrEq(owner, string(w.coordA.ID)) {
			t.Fatalf("owner changed despite refusal: %v", owner)
		}
	})

	t.Run("a terminated coordinator releases its workers to root", func(t *testing.T) {
		if out, code := e.run(t, "session", "assign", string(w.worker.ID), "--owner", string(w.coordB.ID), "--requested-by", root); code != 0 {
			t.Fatalf("assign exit %d: %s", code, out)
		}
		ctx := context.Background()
		store, err := sqlite.Open(e.dataDir)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		coordB, ok, err := store.GetSession(ctx, w.coordB.ID)
		if err != nil || !ok {
			t.Fatalf("load coordinator: ok=%v err=%v", ok, err)
		}
		coordB.IsTerminated = true
		coordB.UpdatedAt = time.Now().UTC().Add(time.Second)
		if err := store.UpdateSession(ctx, coordB); err != nil {
			t.Fatal(err)
		}
		if owner := e.sessionViewOwner(t, w.worker.ID); owner != nil {
			t.Fatalf("worker still owned by terminated coordinator %s", *owner)
		}
		r := e.roster(t, "ao", false)
		if !contains(r.RootWorkerSessionIDs, w.worker.ID) {
			t.Fatalf("released worker not root-owned in roster: %+v", r)
		}
		withDead := e.roster(t, "ao", true)
		found := false
		for _, c := range withDead.Coordinators {
			if c.CoordinatorSessionID == string(w.coordB.ID) && c.IsTerminated {
				found = true
			}
		}
		if !found {
			t.Fatalf("includeTerminated roster lacks the terminated coordinator: %+v", withDead)
		}
	})

	t.Run("remote worker links share the same ownership rules", func(t *testing.T) {
		ctx := context.Background()
		store, err := sqlite.Open(e.dataDir)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		if err := store.UpsertRemoteWorkerLink(ctx, domain.RemoteWorkerLink{
			HostID: "host-build", HostLabel: "build", RemoteSessionID: "widgets-3", RemoteProjectID: "widgets",
			ProjectID: "ao", DisplayName: "remote one", CreatedAt: time.Now().UTC().Truncate(time.Second),
		}); err != nil {
			t.Fatal(err)
		}
		out, code := e.run(t, "remote-worker", "ls", "--project", "ao")
		if code != 0 {
			t.Fatalf("remote-worker ls exit %d: %s", code, out)
		}
		mustContain(t, out, "widgets-3")
		mustContain(t, out, "root")
		if out, code := e.run(t, "remote-worker", "assign", "host-build", "widgets-3", "--owner", string(w.coordA.ID), "--requested-by", root); code != 0 {
			t.Fatalf("remote assign exit %d: %s", code, out)
		}
		out, _ = e.run(t, "remote-worker", "ls", "--project", "ao")
		mustContain(t, out, string(w.coordA.ID))
		for _, bad := range []struct {
			owner, requestedBy, code string
			status                   int
		}{
			{string(w.coordOther.ID), root, "OWNER_CROSS_PROJECT", http.StatusConflict},
			{string(w.coordDead.ID), root, "OWNER_TERMINATED", http.StatusConflict},
			{string(w.worker2.ID), root, "OWNER_NOT_COORDINATOR", http.StatusConflict},
			{string(w.coordA.ID), string(w.coordA.ID), "OWNERSHIP_ROOT_ONLY", http.StatusForbidden},
		} {
			status, _, raw := e.httpJSON(t, "PUT", "remote-worker-links/host-build/widgets-3/owner",
				map[string]any{"ownerSessionId": bad.owner, "requestedBySessionId": bad.requestedBy})
			if status != bad.status || !strings.Contains(string(raw), bad.code) {
				t.Fatalf("remote assign owner=%s by=%s: %d %s, want %d %s", bad.owner, bad.requestedBy, status, raw, bad.status, bad.code)
			}
		}
		if out, code := e.run(t, "remote-worker", "assign", "host-build", "widgets-3", "--owner", "root", "--requested-by", root); code != 0 {
			t.Fatalf("remote clear exit %d: %s", code, out)
		}
		out, _ = e.run(t, "remote-worker", "ls", "--project", "ao")
		mustNotContain(t, out, string(w.coordA.ID))
	})

	t.Run("CLI exit codes: usage 2, daemon error 1 with request id", func(t *testing.T) {
		usage := [][]string{
			// missing --owner
			{"session", "assign", string(w.worker.ID), "--requested-by", root},
			// no caller: AO_SESSION_ID is stripped from the child env
			{"session", "assign", string(w.worker.ID), "--owner", string(w.coordA.ID)},
			// missing worker argument
			{"session", "assign"},
			// no project
			{"coordinator", "ls"},
			// unknown flag
			{"session", "assign", string(w.worker.ID), "--owner", "root", "--bogus"},
		}
		for _, args := range usage {
			if out, code := e.run(t, args...); code != 2 {
				t.Errorf("ao %s: exit %d, want 2: %s", strings.Join(args, " "), code, out)
			}
		}
		out, code := e.run(t, "session", "assign", string(w.worker.ID), "--owner", string(w.coordDead.ID), "--requested-by", root)
		if code != 1 {
			t.Fatalf("daemon rejection exit %d, want 1: %s", code, out)
		}
		mustContain(t, out, "OWNER_TERMINATED")
		m := requestIDPattern.FindStringSubmatch(out)
		if m == nil {
			t.Fatalf("CLI error lost the request id: %q", out)
		}
		out, code = e.run(t, "session", "assign", "nope-1", "--owner", "root", "--requested-by", root)
		if code != 1 || requestIDPattern.FindStringSubmatch(out) == nil {
			t.Fatalf("unknown worker: exit %d, out %q", code, out)
		}
		out, code = e.run(t, "session", "owner", "nope-1")
		if code != 1 || requestIDPattern.FindStringSubmatch(out) == nil {
			t.Fatalf("unknown session owner read: exit %d, out %q", code, out)
		}
	})
}

func sp[T ~string](v T) *string {
	s := string(v)
	return &s
}
