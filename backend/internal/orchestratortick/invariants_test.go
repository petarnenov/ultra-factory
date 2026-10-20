package orchestratortick

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBehindMainNamesTheMissingCommits(t *testing.T) {
	got := texts(Violations(DefaultConfig(), Facts{GitHub: &GitHubState{BehindMain: 2, BehindSHAs: []string{"aaaaaaa", "bbbbbbb"}}}, now))
	want := []string{"dev is 2 commit(s) behind main (aaaaaaa,bbbbbbb) → git merge origin/main into dev and push"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestGreenPRThresholdAndChecks(t *testing.T) {
	cfg := DefaultConfig()
	pr := func(mergeable string, checks ...Check) Facts {
		return Facts{GitHub: &GitHubState{OpenPRs: []PullRequest{{Number: 7, Head: "rw/x", Mergeable: mergeable, Checks: checks}}}}
	}
	old := now.Add(-cfg.GreenPRMax)
	fresh := now.Add(-cfg.GreenPRMax + time.Minute)
	for name, tc := range map[string]struct {
		facts Facts
		want  bool
	}{
		"green long enough":  {pr("MERGEABLE", Check{Result: "SUCCESS", CompletedAt: old}), true},
		"green too recently": {pr("MERGEABLE", Check{Result: "SUCCESS", CompletedAt: old}, Check{Result: "NEUTRAL", CompletedAt: fresh}), false},
		"not mergeable":      {pr("CONFLICTING", Check{Result: "SUCCESS", CompletedAt: old}), false},
		"a check still runs": {pr("MERGEABLE", Check{Result: "SUCCESS", CompletedAt: old}, Check{Result: "IN_PROGRESS"}), false},
		"a check failed":     {pr("MERGEABLE", Check{Result: "FAILURE", CompletedAt: old}), false},
		"no checks":          {pr("MERGEABLE"), false},
		"no completion time": {pr("MERGEABLE", Check{Result: "SUCCESS"}), false},
	} {
		got := len(greenPRsLeftOpen(cfg, tc.facts, now)) == 1
		if got != tc.want {
			t.Errorf("%s: violation = %v, want %v", name, got, tc.want)
		}
	}
}

func TestDraftWithSkippedExpensiveChecksIsNotAGreenPR(t *testing.T) {
	cfg := DefaultConfig()
	old := now.Add(-2 * cfg.GreenPRMax)
	draft := PullRequest{Number: 7, Head: "rw/x", Mergeable: "MERGEABLE", Draft: true, Checks: []Check{
		{Result: "SUCCESS", CompletedAt: old}, {Result: "SKIPPED", CompletedAt: old}, {Result: "SKIPPED", CompletedAt: old},
	}}
	facts := Facts{GitHub: &GitHubState{OpenPRs: []PullRequest{draft}}}
	if got := greenPRsLeftOpen(cfg, facts, now); len(got) != 0 {
		t.Fatalf("draft produced a green-PR violation: %v", got)
	}
	if out := Snapshot(cfg, facts, nil); !strings.Contains(out, "mergeable=NO(draft)") {
		t.Fatalf("snapshot does not mark the draft not mergeable:\n%s", out)
	}
	draft.Draft = false
	facts.GitHub.OpenPRs[0] = draft
	if got := greenPRsLeftOpen(cfg, facts, now); len(got) != 1 {
		t.Fatalf("the same PR once ready must be a violation, got %v", got)
	}
}

func TestSupersededCancelledRunsDoNotOutvoteTheRerun(t *testing.T) {
	t0 := now.Add(-time.Hour)
	run := func(name, wf, result string, at time.Time) Check {
		return Check{Name: name, Workflow: wf, Result: result, CompletedAt: at}
	}
	for name, tc := range map[string]struct {
		checks []Check
		want   string
	}{
		"cancelled then success":   {[]Check{run("go", "Go", "CANCELLED", t0), run("go", "Go", "SUCCESS", t0.Add(time.Minute))}, "green"},
		"order does not matter":    {[]Check{run("go", "Go", "SUCCESS", t0.Add(time.Minute)), run("go", "Go", "CANCELLED", t0)}, "green"},
		"success then failure":     {[]Check{run("go", "Go", "SUCCESS", t0), run("go", "Go", "FAILURE", t0.Add(time.Minute))}, "red"},
		"success then running":     {[]Check{run("go", "Go", "SUCCESS", t0), run("go", "Go", "IN_PROGRESS", time.Time{})}, "pending"},
		"running then cancelled":   {[]Check{run("go", "Go", "IN_PROGRESS", time.Time{}), run("go", "Go", "CANCELLED", t0)}, "pending"},
		"other workflow same name": {[]Check{run("check", "Policy", "FAILURE", t0), run("check", "Mobile", "SUCCESS", t0.Add(time.Minute))}, "red"},
		"no workflow falls back":   {[]Check{run("lint", "", "CANCELLED", t0), run("lint", "", "SUCCESS", t0.Add(time.Minute))}, "green"},
		"unnamed checks all count": {[]Check{run("", "", "CANCELLED", t0), run("", "", "SUCCESS", t0.Add(time.Minute))}, "red"},
	} {
		if got := CheckSummary(tc.checks); got != tc.want {
			t.Errorf("%s: CheckSummary = %s, want %s", name, got, tc.want)
		}
	}
}

func TestGreenPRViolationMatchesReviewState(t *testing.T) {
	cfg := DefaultConfig()
	greenFacts := func(mutate func(*PullRequest, *Worker)) Facts {
		pr := PullRequest{
			Number: 201, Head: "rw/x", HeadSHA: head("201"),
			Mergeable: "MERGEABLE", ReviewDecision: "APPROVED",
			Checks: []Check{{Result: "SUCCESS", CompletedAt: now.Add(-20 * time.Minute)}},
		}
		worker := Worker{ID: "uf-1", Branch: "rw/x", Reviews: []WorkerReview{
			{PRNumber: 201, TargetSHA: pr.HeadSHA, Status: "up_to_date"},
		}}
		if mutate != nil {
			mutate(&pr, &worker)
		}
		return Facts{
			GitHub: &GitHubState{OpenPRs: []PullRequest{pr}},
			Hosts:  []HostSessions{{Host: "Linux", Sessions: []Worker{worker}}},
		}
	}
	const base = `#201 (rw/x) has been green and mergeable for 20 min → review it per "Review and merge"`
	cases := map[string]struct {
		mutate func(*PullRequest, *Worker)
		want   []string
	}{
		"approved": {nil, []string{base + " and merge it"}},
		"AO review running": {func(_ *PullRequest, w *Worker) {
			w.Reviews[0].Status = "running"
		}, []string{base + "; AO review is running"}},
		"AO review timed out": {func(_ *PullRequest, w *Worker) {
			w.Reviews[0].Status = "needs_review"
		}, []string{base + "; AO review is needs_review"}},
		"no AO review for this head": {func(_ *PullRequest, w *Worker) {
			w.Reviews = nil
		}, []string{base + "; no current-head AO native approval"}},
		"stale AO review for another head": {func(_ *PullRequest, w *Worker) {
			w.Reviews[0].TargetSHA = head("stale")
		}, []string{base + "; no current-head AO native approval"}},
		"provider approval required": {func(pr *PullRequest, _ *Worker) {
			pr.ReviewDecision = "REVIEW_REQUIRED"
		}, []string{base + "; review approval required"}},
		"AO changes requested": {func(_ *PullRequest, w *Worker) {
			w.Reviews[0].Status = "changes_requested"
		}, []string{}},
		"provider changes requested": {func(pr *PullRequest, _ *Worker) {
			pr.ReviewDecision = "CHANGES_REQUESTED"
		}, []string{}},
		"unresolved review thread": {func(pr *PullRequest, _ *Worker) {
			pr.Threads = []ReviewThread{{Body: "fix this"}}
		}, []string{}},
		"failing checks": {func(pr *PullRequest, _ *Worker) {
			pr.Checks = []Check{{Result: "FAILURE", CompletedAt: now.Add(-20 * time.Minute)}}
		}, []string{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := texts(greenPRsLeftOpen(cfg, greenFacts(tc.mutate), now))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGreenPRHoldLabelIsNotAMergeViolation(t *testing.T) {
	cfg := DefaultConfig()
	facts := func(labels []string, reviewDecision string, reviews []WorkerReview) Facts {
		pr := PullRequest{
			Number: 354, Head: "fix/daemon-tunnel-comment-policy", HeadSHA: head("354"),
			Mergeable: "MERGEABLE", Labels: labels, ReviewDecision: reviewDecision,
			Checks: []Check{{Result: "SUCCESS", CompletedAt: now.Add(-30 * time.Minute)}},
		}
		return Facts{
			GitHub: &GitHubState{OpenPRs: []PullRequest{pr}},
			Hosts: []HostSessions{{Host: "Linux", Sessions: []Worker{
				{ID: "uf-1", Branch: "fix/daemon-tunnel-comment-policy", Reviews: reviews},
			}}},
		}
	}
	const base = `#354 (fix/daemon-tunnel-comment-policy) has been green and mergeable for 30 min → review it per "Review and merge"`
	currentHead := []WorkerReview{{PRNumber: 354, TargetSHA: head("354"), Status: "up_to_date"}}
	cases := map[string]struct {
		labels         []string
		reviewDecision string
		reviews        []WorkerReview
		want           []string
	}{
		"no AO review with needs-human": {labels: []string{"needs-human"}, want: []string{}},
		"approved with needs-human": {
			labels: []string{"needs-human"}, reviewDecision: "APPROVED", reviews: currentHead,
			want: []string{},
		},
		"status:blocked is also a hold": {labels: []string{"status:blocked"}, want: []string{}},
		"do-not-merge is also a hold":   {labels: []string{"do-not-merge"}, want: []string{}},
		"hold label match ignores case": {labels: []string{"NEEDS-HUMAN"}, want: []string{}},
		"no hold label keeps the review wording": {
			want: []string{base + "; no current-head AO native approval"},
		},
		"running review is the review wording": {
			reviews: []WorkerReview{{PRNumber: 354, TargetSHA: head("354"), Status: "running"}},
			want:    []string{base + "; AO review is running"},
		},
		"approved without a hold label instructs the merge": {
			reviewDecision: "APPROVED", reviews: currentHead,
			want: []string{base + " and merge it"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := texts(greenPRsLeftOpen(cfg, facts(tc.labels, tc.reviewDecision, tc.reviews), now))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMergedPRWithOpenIssue(t *testing.T) {
	gh := &GitHubState{
		MergedPRs:  []MergedPR{{Number: 190, Body: "Closes #120, fixes #5 and resolves #120"}, {Number: 191, Body: "Refs #120"}},
		OpenIssues: map[int]string{120: "RW-4.1", 5: "RW-1.1", 9: "RW-9"},
	}
	got := texts(mergedPRsWithOpenIssues(gh))
	want := []string{`#190 is merged but #5, #120 is still open → gh issue close per step 5 of "Review and merge"`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestIdleWorkerWithoutPR(t *testing.T) {
	cfg := DefaultConfig()
	idle := now.Add(-cfg.IdleWorkerMax)
	hosts := []HostSessions{{Host: "Z600", Sessions: []Worker{
		{ID: "a", Branch: "rw/a", Activity: "idle", LastActivityAt: idle},
		{ID: "b", Branch: "rw/b", Activity: "idle", LastActivityAt: idle, PRNumber: 3},
		{ID: "c", Branch: "rw/c", Activity: "active", LastActivityAt: idle},
		{ID: "d", Branch: "rw/d", Activity: "idle", LastActivityAt: idle, Terminated: true},
		{ID: "e", Branch: "ao/e/root", Activity: "idle", LastActivityAt: idle},
		{ID: "f", Branch: "rw/f", Activity: "idle", LastActivityAt: now.Add(-time.Minute)},
	}}}
	got := texts(idleWorkersWithoutPR(cfg, hosts, now))
	if len(got) != 1 || !strings.HasPrefix(got[0], "a (rw/a) on Z600 has been idle for 30 min without a PR") {
		t.Fatalf("got %q", got)
	}
}

func TestAgentMix(t *testing.T) {
	cfg := DefaultConfig()
	after := cfg.AgentMixSince.Add(time.Hour)
	hosts := []HostSessions{
		{Host: "Linux", Sessions: []Worker{
			{ID: "l1", Branch: "rw/1", Harness: "claude-code", CreatedAt: after},
			{ID: "l2", Branch: "rw/2", Harness: "claude-code", CreatedAt: after},
			{ID: "l3", Branch: "rw/3", Harness: "claude-code", CreatedAt: after, Terminated: true},
		}},
		{Host: "IMac", Sessions: []Worker{
			{ID: "i1", Branch: "rw/4", Harness: "claude-code", CreatedAt: cfg.AgentMixSince.Add(-time.Hour)},
		}},
	}
	got := texts(agentMix(cfg, hosts))
	want := []string{
		"Linux runs 2 live claude-code workers (l1, l2) → the next task on Linux must use the other agent (--harness)",
		"none of the 3 workers started since 2026-10-16T15:45:00Z uses codex → start the next ready task with --harness codex",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
	hosts[0].Sessions[1].Harness = "codex"
	if got := agentMix(cfg, hosts); len(got) != 0 {
		t.Fatalf("balanced mix: %q", got)
	}
}

func TestCheckSummary(t *testing.T) {
	for want, checks := range map[string][]Check{
		"none":    nil,
		"green":   {{Result: "SUCCESS"}, {Result: "SKIPPED"}},
		"pending": {{Result: "SUCCESS"}, {Result: "QUEUED"}},
		"red":     {{Result: "QUEUED"}, {Result: "CANCELLED"}},
	} {
		if got := CheckSummary(checks); got != want {
			t.Errorf("CheckSummary(%v) = %s, want %s", checks, got, want)
		}
	}
}

func TestConfigFromEnv(t *testing.T) {
	defaults, err := ConfigFromEnv(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Interval != 3*time.Minute {
		t.Fatalf("default interval = %s, want 3m", defaults.Interval)
	}

	env := map[string]string{
		"RW_TICK_PROJECT": "p", "RW_TICK_HOSTS": "A  B", "RW_TICK_INTERVAL": "60",
		"RW_TICK_RENUDGE_MINUTES": "5", "RW_TICK_PAUSED": "1", "RW_TICK_REPO": "o/r",
	}
	cfg, err := ConfigFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project != "p" || !reflect.DeepEqual(cfg.Hosts, []string{"A", "B"}) || cfg.Interval != time.Minute ||
		cfg.Renudge != 5*time.Minute || !cfg.Paused || cfg.Repo != "o/r" || cfg.Base != "dev" {
		t.Fatalf("cfg = %+v", cfg)
	}
	for name, bad := range map[string]map[string]string{
		"interval":  {"RW_TICK_INTERVAL": "soon"},
		"zero":      {"RW_TICK_RENUDGE_MINUTES": "0"},
		"repo":      {"RW_TICK_REPO": "no-slash"},
		"mix since": {"RW_TICK_AGENT_MIX_SINCE": "yesterday"},
	} {
		if _, err := ConfigFromEnv(func(k string) string { return bad[k] }); err == nil {
			t.Errorf("%s: invalid configuration accepted", name)
		}
	}
}

func texts(vs []Violation) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Text
	}
	return out
}

// A violation whose duration grows is still the same state: its fingerprint
// does not change, so it is re-sent on the re-nudge interval, not every tick.
func TestFingerprintIgnoresGrowingDurations(t *testing.T) {
	cfg := DefaultConfig()
	facts := Facts{GitHub: &GitHubState{OpenPRs: []PullRequest{{Number: 7, Head: "rw/x", Mergeable: "MERGEABLE",
		Checks: []Check{{Result: "SUCCESS", CompletedAt: now.Add(-time.Hour)}}}}}}
	early, late := Violations(cfg, facts, now), Violations(cfg, facts, now.Add(5*time.Minute))
	if Snapshot(cfg, facts, early) == Snapshot(cfg, facts, late) {
		t.Fatal("the shown durations should differ")
	}
	if Fingerprint(cfg, facts, early) != Fingerprint(cfg, facts, late) {
		t.Fatal("the fingerprint changed although only the duration grew")
	}
}
