package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/orchestratortick"
)

func TestTickGitHubStateMapsTheGraphQLResponse(t *testing.T) {
	raw := `{
	  "repository": {
	    "open": {"nodes": [{
	      "number": 201, "headRefName": "rw/RW-5.4.2", "mergeable": "MERGEABLE",
	      "readyEvents": {"nodes": [{"createdAt": "2026-10-20T18:50:00Z"}]},
	      "commits": {"nodes": [{"commit": {"statusCheckRollup": {"contexts": {"nodes": [
	        {"__typename": "CheckRun", "name": "lint", "checkSuite": {"workflowRun": {"workflow": {"name": "Lint"}}}, "status": "COMPLETED", "conclusion": "SUCCESS", "completedAt": "2026-10-17T11:20:00Z"},
	        {"__typename": "CheckRun", "status": "IN_PROGRESS", "conclusion": null, "completedAt": null},
	        {"__typename": "StatusContext", "state": "SUCCESS", "createdAt": "2026-10-17T11:25:00Z"}
	      ]}}}}]}
	    }, {
	      "number": 202, "headRefName": "rw/x", "mergeable": "UNKNOWN",
	      "commits": {"nodes": [{"commit": {"statusCheckRollup": null}}]}
	    }]},
	    "merged": {"nodes": [{"number": 190, "body": "Closes #120"}]},
	    "issues": {"nodes": [{"number": 120, "title": "RW-4.1"}]},
	    "ref": {"compare": {"aheadBy": 2, "commits": {"nodes": [{"oid": "aaaaaaa1111"}, {"oid": "bbbbbbb2222"}]}}}
	  }
	}`
	var resp tickGitHubResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	got, err := tickGitHubState(resp, orchestratortick.GitHubQuery{Repo: "o/r", Base: "feat/b"})
	if err != nil {
		t.Fatal(err)
	}
	want := orchestratortick.GitHubState{
		OpenPRs: []orchestratortick.PullRequest{
			{Number: 201, Head: "rw/RW-5.4.2", Mergeable: "MERGEABLE",
				ReadyAt: time.Date(2026, 10, 20, 18, 50, 0, 0, time.UTC), Checks: []orchestratortick.Check{
					{Name: "lint", Workflow: "Lint", Result: "SUCCESS", CompletedAt: time.Date(2026, 10, 17, 11, 20, 0, 0, time.UTC)},
					{Result: "IN_PROGRESS"},
					{Result: "SUCCESS", CompletedAt: time.Date(2026, 10, 17, 11, 25, 0, 0, time.UTC)},
				}},
			{Number: 202, Head: "rw/x", Mergeable: "UNKNOWN"},
		},
		MergedPRs:  []orchestratortick.MergedPR{{Number: 190, Body: "Closes #120"}},
		OpenIssues: map[int]string{120: "RW-4.1"},
		BehindMain: 2,
		BehindSHAs: []string{"aaaaaaa", "bbbbbbb"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	if !strings.Contains(tickGitHubQuery, "readyEvents: timelineItems(last: 1, itemTypes: [READY_FOR_REVIEW_EVENT])") {
		t.Fatal("query does not alias the ready-for-review event as readyEvents")
	}
	if orchestratortick.CheckSummary(got.OpenPRs[0].Checks) != "pending" {
		t.Fatal("a running check must keep the PR pending")
	}

	if _, err := tickGitHubState(tickGitHubResponse{}, orchestratortick.GitHubQuery{Repo: "o/r"}); err == nil || !strings.Contains(err.Error(), "repository o/r not found") {
		t.Fatalf("missing repository: %v", err)
	}
	resp.Repository.Ref = nil
	if _, err := tickGitHubState(resp, orchestratortick.GitHubQuery{Repo: "o/r", Base: "feat/b"}); err == nil || !strings.Contains(err.Error(), "branch feat/b not found") {
		t.Fatalf("missing branch: %v", err)
	}
}

// tickDaemon speaks the daemon API the same way the ao commands do.
func TestTickDaemonUsesTheDaemonAPI(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "")
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/orchestrators":
			_, _ = w.Write([]byte(`{"sessions":[
			  {"id":"uf-orchestrator-7","projectId":"ultra-factory","kind":"orchestrator"},
			  {"id":"uf-orchestrator-6","projectId":"ultra-factory","kind":"orchestrator","isTerminated":true},
			  {"id":"uf-3","projectId":"ultra-factory","kind":"worker"}]}`))
		case r.URL.Path == "/api/v1/sessions/uf-orchestrator-7" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"session":{"id":"uf-orchestrator-7","status":"idle","activity":{"state":"idle"}}}`))
		case r.URL.Path == "/api/v1/sessions/uf-orchestrator-7/send":
			_ = json.NewDecoder(r.Body).Decode(&sent)
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	writeRunFileFor(t, cfg, srv)
	c := &commandContext{deps: Deps{ProcessAlive: func(int) bool { return true }}.withDefaults()}
	d := tickDaemon{c: c}

	orchs, err := d.Orchestrators(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(orchs, []orchestratortick.Orchestrator{{ID: "uf-orchestrator-7", ProjectID: "ultra-factory"}}) {
		t.Fatalf("orchestrators = %+v", orchs)
	}
	st, err := d.Session(context.Background(), "uf-orchestrator-7")
	if err != nil || st.Status != "idle" || st.Activity != "idle" {
		t.Fatalf("session = %+v, %v", st, err)
	}
	if err := d.Send(context.Background(), "uf-orchestrator-7", "TICK"); err != nil {
		t.Fatal(err)
	}
	if sent["message"] != "TICK" {
		t.Fatalf("sent body = %+v", sent)
	}
}

func TestTickPauseResumeAndStatus(t *testing.T) {
	setConfigEnv(t)
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	deps := Deps{Now: func() time.Time { return now }}
	out, _, err := executeCLI(t, deps, "orchestrator", "tick", "pause", "redeploy")
	if err != nil || !strings.HasPrefix(out, "paused: redeploy (by ") {
		t.Fatalf("pause: %q %v", out, err)
	}
	out, _, err = executeCLI(t, deps, "orchestrator", "tick", "status")
	if err != nil || !strings.Contains(out, "paused: redeploy") || !strings.Contains(out, "last tick: none") || !strings.Contains(out, "last sent: none") {
		t.Fatalf("status: %q %v", out, err)
	}
	if out, _, err = executeCLI(t, deps, "orchestrator", "tick", "resume"); err != nil || out != "resumed\n" {
		t.Fatalf("resume: %q %v", out, err)
	}
	if out, _, err = executeCLI(t, deps, "orchestrator", "tick", "resume"); err != nil || out != "not paused\n" {
		t.Fatalf("second resume: %q %v", out, err)
	}
}

func TestTickRejectsAnInvalidConfiguration(t *testing.T) {
	setConfigEnv(t)
	t.Setenv("RW_TICK_INTERVAL", "soon")
	_, _, err := executeCLI(t, Deps{}, "orchestrator", "tick", "run", "--dry-run")
	if err == nil || !strings.Contains(err.Error(), "RW_TICK_INTERVAL must be a positive integer") {
		t.Fatalf("err = %v", err)
	}
}

func TestTickHoldAndReleasePersistAnObligation(t *testing.T) {
	setConfigEnv(t)
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	out, _, err := executeCLI(t, Deps{Now: func() time.Time { return now }}, "orchestrator", "tick", "hold", "uf-1", "--host", "Linux", "--reason", "deployment pending", "--until", "live check passes")
	if err != nil || !strings.Contains(out, "hold Linux/uf-1: deployment pending") {
		t.Fatalf("hold: %q %v", out, err)
	}
	out, _, err = executeCLI(t, Deps{Now: func() time.Time { return now }}, "orchestrator", "tick", "status")
	if err != nil || !strings.Contains(out, "completion: Linux/uf-1") || !strings.Contains(out, "obligation: deployment pending (until live check passes)") {
		t.Fatalf("status after hold: %q %v", out, err)
	}
	if out, _, err = executeCLI(t, Deps{Now: func() time.Time { return now }}, "orchestrator", "tick", "release", "uf-1", "--host", "Linux"); err != nil || out != "released Linux/uf-1\n" {
		t.Fatalf("release: %q %v", out, err)
	}
	out, _, err = executeCLI(t, Deps{}, "orchestrator", "tick", "status")
	if err != nil || strings.Contains(out, "obligation:") {
		t.Fatalf("status after release: %q %v", out, err)
	}
}

func TestTickKillVerifiesTheTerminatedFact(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "")
	terminated := false
	kills := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/sessions/uf-1" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"session":{"id":"uf-1","isTerminated":` + boolString(terminated) + `}}`))
		case r.URL.Path == "/api/v1/sessions/uf-1/kill" && r.Method == http.MethodPost:
			kills++
			terminated = true
			_, _ = w.Write([]byte(`{"sessionId":"uf-1","freed":false}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	writeRunFileFor(t, cfg, srv)
	c := &commandContext{deps: Deps{ProcessAlive: func(int) bool { return true }}.withDefaults()}
	if err := (tickDaemon{c: c}).Kill(context.Background(), "", "uf-1"); err != nil {
		t.Fatal(err)
	}
	if kills != 1 {
		t.Fatalf("kills = %d, want 1", kills)
	}
	if err := (tickDaemon{c: c}).Kill(context.Background(), "", "uf-1"); err != nil {
		t.Fatal(err)
	}
	if kills != 1 {
		t.Fatalf("an already terminated session was killed again: %d", kills)
	}
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func TestTickGitHubStateMapsMergeReadinessFacts(t *testing.T) {
	raw := `{
	  "repository": {
	    "open": {"nodes": [{
	      "number": 201, "headRefName": "rw/190", "headRefOid": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	      "isDraft": false, "mergeable": "MERGEABLE", "reviewDecision": "APPROVED",
	      "labels": {"nodes": [{"name": "rw"}]},
	      "commits": {"nodes": [{"commit": {"statusCheckRollup": {"contexts": {"nodes": [
	        {"__typename": "CheckRun", "status": "COMPLETED", "conclusion": "SUCCESS", "completedAt": "2026-10-17T11:20:00Z"}
	      ]}}}}]},
	      "reviewThreads": {"nodes": [{"isResolved": true, "isOutdated": false, "comments": {"nodes": [{"body": "fixed", "author": {"login": "reviewer", "__typename": "User"}}]}}]}
	    }]},
	    "merged": {"nodes": [{"number": 199, "headRefName": "rw/198", "headRefOid": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "body": "Closes #198", "mergedAt": "2026-10-17T11:00:00Z"}]},
	    "closed": {"nodes": [{"number": 197, "headRefName": "rw/197", "headRefOid": "cccccccccccccccccccccccccccccccccccccccc", "closedAt": "2026-10-17T10:00:00Z"}]},
	    "issues": {"nodes": []},
	    "ref": {"compare": {"aheadBy": 0, "commits": {"nodes": []}}}
	  }
	}`
	var resp tickGitHubResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	got, err := tickGitHubState(resp, orchestratortick.GitHubQuery{Repo: "o/r", Base: "feat/b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.OpenPRs) != 1 || got.OpenPRs[0].HeadSHA != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ||
		got.OpenPRs[0].ReviewDecision != "APPROVED" || len(got.OpenPRs[0].Labels) != 1 ||
		len(got.OpenPRs[0].Threads) != 1 || !got.OpenPRs[0].Threads[0].Resolved {
		t.Fatalf("open PR = %+v", got.OpenPRs)
	}
	if len(got.MergedPRs) != 1 || got.MergedPRs[0].Head != "rw/198" || got.MergedPRs[0].HeadSHA == "" {
		t.Fatalf("merged PRs = %+v", got.MergedPRs)
	}
	if len(got.ClosedPRs) != 1 || got.ClosedPRs[0].Head != "rw/197" {
		t.Fatalf("closed PRs = %+v", got.ClosedPRs)
	}
}

func TestTickDaemonWorkersCarryEveryOwnedPRAndBranchState(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/sessions":
			if r.URL.Query().Get("active") != "true" {
				http.NotFound(w, r)
				return
			}
		case "/api/v1/sessions/uf-1/reviews":
			_, _ = w.Write([]byte(`{"reviews":[{"prNumber":202,"targetSha":"sha-202","status":"up_to_date"}]}`))
			return
		default:
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"sessions":[{
		  "id":"uf-1","projectId":"ultra-factory","kind":"worker","branch":"rw/190",
		  "activity":{"state":"idle"},"harness":"opencode",
		  "branchState":{"commits":2,"remoteBranch":"origin/rw/190","unpushed":1},
		  "prs":[{"number":201,"state":"merged"},{"number":202,"state":"open"}]
		}]}`))
	}))
	defer srv.Close()
	writeRunFileFor(t, cfg, srv)
	c := &commandContext{deps: Deps{ProcessAlive: func(int) bool { return true }}.withDefaults()}
	workers, err := (tickDaemon{c: c}).Workers(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(workers) != 1 {
		t.Fatalf("workers = %+v", workers)
	}
	w := workers[0]
	if w.Kind != "worker" || len(w.PRs) != 2 || w.PRs[0].Number != 201 || w.PRs[1].State != "open" {
		t.Fatalf("worker PRs = %+v", w)
	}
	if w.BranchState == nil || w.BranchState.Unpushed != 1 || !w.BranchState.Known {
		t.Fatalf("branch state = %+v", w.BranchState)
	}
	if len(w.Reviews) != 1 || w.Reviews[0].PRNumber != 202 || w.Reviews[0].Status != "up_to_date" {
		t.Fatalf("reviews = %+v", w.Reviews)
	}
}

func TestTickStatusShowsMergeActionsAndBlockers(t *testing.T) {
	store := orchestratortick.FileStore{Dir: t.TempDir()}
	state := orchestratortick.CompletionState{Sessions: map[string]orchestratortick.SessionCompletion{
		"Linux/worker": {Host: "Linux", Session: "worker", Remaining: []string{"PR #4: hold label needs-human"}, Actions: map[string]orchestratortick.CompletionAction{
			"a": {Kind: "merge", Status: "pending", PR: 1},
			"b": {Kind: "merge", Status: "completed", PR: 2, MergeCommitSHA: "commit-2"},
			"c": {Kind: "merge", Status: "blocked", PR: 3, LastError: "required checks are pending"},
			"d": {Kind: "merge", Status: "superseded", PR: 5, LastError: "head changed"},
		}},
	}}
	if err := store.SaveCompletion(state); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := writeTickStatus(&out, store); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"PR #1 pending", "PR #2 completed", "commit=commit-2", "PR #3 blocked", "required checks are pending", "hold label needs-human", "head changed"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in %s", want, out.String())
		}
	}
}

func TestTickCompletionNotificationUsesHubAndSavedHostIdentity(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "")
	remote := newFakeRemoteHost(t, "host-build", "pw")
	writeRemotesFile(t, remoteEntry{Label: "build", URL: remote.srv.URL, Password: "pw", HostID: "host-build"})
	var input struct {
		Recipient *orchestratortick.CompletionRecipient `json:"recipient"`
		HostID    string                                `json:"hostId"`
		SessionID string                                `json:"sessionId"`
		ActionKey string                                `json:"actionKey"`
		PRs       []orchestratortick.CompletedPR        `json:"prs"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/remote-worker-links" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"links":[{"hostId":"host-build","remoteSessionId":"worker","projectId":"project","ownerSessionId":"local-coordinator"}]}`))
			return
		}
		if r.URL.Path != "/api/v1/reports/completion" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accepted":true}`))
	}))
	defer srv.Close()
	writeRunFileFor(t, cfg, srv)
	d := tickDaemon{c: &commandContext{deps: Deps{ProcessAlive: func(int) bool { return true }}.withDefaults()}}
	recipient, err := d.ResolveCompletionOwner(context.Background(), "build", "worker")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.NotifyCompletion(context.Background(), "build", "worker", "merge:201@head", "merge", []orchestratortick.CompletedPR{{Number: 201, MergeCommitSHA: "commit"}}, &recipient); err != nil {
		t.Fatal(err)
	}
	if input.Recipient == nil || input.Recipient.OwnerSessionID != "local-coordinator" || input.Recipient.ProjectID != "project" || input.HostID != "host-build" || input.SessionID != "worker" || input.ActionKey != "merge:201@head" || len(input.PRs) != 1 || input.PRs[0].MergeCommitSHA != "commit" {
		t.Fatalf("notification: %+v", input)
	}
	if len(remote.recorded()) != 0 {
		t.Fatal("owner notification contacted the worker host")
	}
}
