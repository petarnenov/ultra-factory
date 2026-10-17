package cli

import (
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
	      "commits": {"nodes": [{"commit": {"statusCheckRollup": {"contexts": {"nodes": [
	        {"__typename": "CheckRun", "status": "COMPLETED", "conclusion": "SUCCESS", "completedAt": "2026-10-17T11:20:00Z"},
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
			{Number: 201, Head: "rw/RW-5.4.2", Mergeable: "MERGEABLE", Checks: []orchestratortick.Check{
				{Result: "SUCCESS", CompletedAt: time.Date(2026, 10, 17, 11, 20, 0, 0, time.UTC)},
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
