package orchestratortick

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeMerger struct {
	mu       sync.Mutex
	requests []MergeRequest
	err      error
}

func (m *fakeMerger) Merge(_ context.Context, request MergeRequest) (MergeResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, request)
	if m.err != nil {
		return MergeResult{}, m.err
	}
	return MergeResult{MergeCommitSHA: "merge-" + request.ExpectedHeadSHA[:7]}, nil
}

func (m *fakeMerger) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

func head(seed string) string {
	if len(seed) >= 40 {
		return seed[:40]
	}
	return seed + strings.Repeat("a", 40-len(seed))
}

func readyPR(number int, branch, sha string) PullRequest {
	return PullRequest{
		Number: number, Head: branch, HeadSHA: sha, Mergeable: "MERGEABLE",
		ReviewDecision: "APPROVED",
		Checks:         []Check{{Result: "SUCCESS", CompletedAt: now.Add(-time.Hour)}},
	}
}

func cleanWorker(br string, prs ...int) Worker {
	w := Worker{
		ID: "uf-1", Kind: "worker", Branch: br, Activity: "idle",
		BranchState: &WorkerBranchState{Known: true}, CreatedAt: now.Add(-time.Hour), LastActivityAt: now.Add(-time.Minute),
	}
	for _, pr := range prs {
		w.PRs = append(w.PRs, WorkerPR{Number: pr, State: "open"})
	}
	return w
}

func completionFixture(t *testing.T) (*Runner, *fakeDaemon, *fakeGitHub, *fakeMerger, FileStore) {
	t.Helper()
	r, d, gh, store := fixture(t)
	fm := &fakeMerger{}
	r.Merger = fm
	r.Config.MergeAuthorized = true
	r.Config.CloseCompleted = true
	worker := cleanWorker("rw/190", 201)
	worker.Reviews = []WorkerReview{{PRNumber: 201, TargetSHA: head("1"), Status: "up_to_date"}}
	d.workers["Linux"] = []Worker{worker}
	gh.state = GitHubState{OpenPRs: []PullRequest{readyPR(201, "rw/190", head("1"))}}
	return r, d, gh, fm, store
}

func TestReadyAuthorizedPRMergesOnceThenClosesTheCleanWorker(t *testing.T) {
	r, d, gh, fm, store := completionFixture(t)

	first := tick(t, r)
	if fm.calls() != 1 {
		t.Fatalf("merge calls = %d, want 1", fm.calls())
	}
	if fm.requests[0].Number != 201 || fm.requests[0].ExpectedHeadSHA != head("1") {
		t.Fatalf("merge request = %+v", fm.requests[0])
	}
	if len(d.killed) != 0 {
		t.Fatalf("worker closed while its PR was still open: %v", d.killed)
	}
	if !strings.Contains(strings.Join(CompletionLines(first.Completions), " "), "prs=#201") {
		t.Fatalf("completion note = %+v", first.Completions)
	}

	// The next provider observation reports the same PR merged.
	gh.state = GitHubState{MergedPRs: []MergedPR{{Number: 201, Head: "rw/190", HeadSHA: head("1")}}}
	tick(t, r)
	if fm.calls() != 1 {
		t.Fatalf("repeated tick merged again: calls = %d", fm.calls())
	}
	if len(d.killed) != 1 || d.killed[0] != "Linux/uf-1" {
		t.Fatalf("killed = %v, want one host-qualified close", d.killed)
	}
	state, _, err := store.Completion()
	if err != nil {
		t.Fatal(err)
	}
	session := state.Sessions["Linux/uf-1"]
	if session.Actions["close"].Status != CompletionActionCompleted {
		t.Fatalf("close action = %+v", session.Actions["close"])
	}

	// A third tick with an already completed action must not repeat the close.
	tick(t, r)
	if len(d.killed) != 1 {
		t.Fatalf("completed close replayed: %v", d.killed)
	}
}

func TestMissingMergeAuthorizationIsExplicit(t *testing.T) {
	r, _, _, fm, _ := completionFixture(t)
	r.Config.MergeAuthorized = false
	res := tick(t, r)
	if fm.calls() != 0 {
		t.Fatal("merged without authorization")
	}
	lines := strings.Join(CompletionLines(res.Completions), " ")
	if !strings.Contains(lines, "merge authorization missing") {
		t.Fatalf("completion lines = %q", lines)
	}
}

func TestHoldLabelBlocksMergeAndNamesTheBlocker(t *testing.T) {
	r, _, gh, fm, _ := completionFixture(t)
	pr := readyPR(201, "rw/190", head("1"))
	pr.Labels = []string{"needs-human"}
	gh.state.OpenPRs = []PullRequest{pr}
	res := tick(t, r)
	if fm.calls() != 0 {
		t.Fatal("merged a held PR")
	}
	lines := strings.Join(CompletionLines(res.Completions), " ")
	if !strings.Contains(lines, "hold label needs-human") {
		t.Fatalf("completion lines = %q", lines)
	}
}

func TestHeadChangeBetweenCheckAndMutationIsNotRetried(t *testing.T) {
	r, _, gh, fm, store := completionFixture(t)
	fm.err = ErrMergeHeadChanged
	tick(t, r)
	if fm.calls() != 1 {
		t.Fatalf("merge calls = %d, want 1", fm.calls())
	}
	state, _, _ := store.Completion()
	session := state.Sessions["Linux/uf-1"]
	var blocked CompletionAction
	for key, action := range session.Actions {
		if strings.HasPrefix(key, "merge:201@") {
			blocked = action
		}
	}
	if blocked.Status != CompletionActionSuperseded || !strings.Contains(blocked.LastError, "head changed") {
		t.Fatalf("merge action = %+v", blocked)
	}

	// The provider now has a different head. The queued action must not merge
	// the new head using the old approval.
	gh.state.OpenPRs[0].HeadSHA = head("2")
	gh.state.OpenPRs[0].ReviewDecision = ""
	tick(t, r)
	if fm.calls() != 1 {
		t.Fatalf("a stale head was merged again: calls = %d", fm.calls())
	}
}

func TestUnpublishedCommitsPreventClosure(t *testing.T) {
	r, d, gh, _, _ := completionFixture(t)
	gh.state = GitHubState{MergedPRs: []MergedPR{{Number: 201, Head: "rw/190", HeadSHA: head("1")}}}
	worker := d.workers["Linux"][0]
	worker.BranchState = &WorkerBranchState{Known: true, Commits: 2, Unpushed: 2}
	d.workers["Linux"] = []Worker{worker}
	res := tick(t, r)
	if len(d.killed) != 0 {
		t.Fatal("closed a worker with unpublished commits")
	}
	if !strings.Contains(strings.Join(CompletionLines(res.Completions), " "), "2 unpublished commit(s)") {
		t.Fatalf("completion = %+v", res.Completions)
	}
}

func TestUnknownBranchStatePreventsClosure(t *testing.T) {
	r, d, gh, _, _ := completionFixture(t)
	gh.state = GitHubState{MergedPRs: []MergedPR{{Number: 201, Head: "rw/190", HeadSHA: head("1")}}}
	worker := d.workers["Linux"][0]
	worker.BranchState = nil
	d.workers["Linux"] = []Worker{worker}
	res := tick(t, r)
	if len(d.killed) != 0 {
		t.Fatal("closed a worker without proving its branch push state")
	}
	if !strings.Contains(strings.Join(CompletionLines(res.Completions), " "), "branch push state is unknown") {
		t.Fatalf("completion = %+v", res.Completions)
	}
}

func TestMultipleOwnedPRsPreventClosure(t *testing.T) {
	r, d, gh, _, _ := completionFixture(t)
	worker := d.workers["Linux"][0]
	worker.PRs = append(worker.PRs, WorkerPR{Number: 202, State: "open"})
	d.workers["Linux"] = []Worker{worker}
	gh.state = GitHubState{
		MergedPRs: []MergedPR{{Number: 201, Head: "rw/190", HeadSHA: head("1")}},
		OpenPRs:   []PullRequest{readyPR(202, "rw/190/child", head("2"))},
	}
	res := tick(t, r)
	if len(d.killed) != 0 {
		t.Fatal("closed a worker that still owns an open PR")
	}
	if !strings.Contains(strings.Join(CompletionLines(res.Completions), " "), "owned PR #202 is still open") {
		t.Fatalf("completion = %+v", res.Completions)
	}
}

func TestOrchestratorIsNeverClosed(t *testing.T) {
	r, d, gh, _, _ := completionFixture(t)
	worker := d.workers["Linux"][0]
	worker.Kind = "orchestrator"
	d.workers["Linux"] = []Worker{worker}
	gh.state = GitHubState{MergedPRs: []MergedPR{{Number: 201, Head: "rw/190", HeadSHA: head("1")}}}
	tick(t, r)
	if len(d.killed) != 0 {
		t.Fatal("closed an orchestrator session")
	}
}

func TestObligationKeepsTheWorkerOpenUntilReleased(t *testing.T) {
	r, d, gh, _, store := completionFixture(t)
	gh.state = GitHubState{MergedPRs: []MergedPR{{Number: 201, Head: "rw/190", HeadSHA: head("1")}}}
	state, _, _ := store.Completion()
	state.Sessions["Linux/uf-1"] = SessionCompletion{
		Host: "Linux", Session: "uf-1",
		Obligations: []Obligation{{ID: "deploy", Reason: "deployment pending", CompletionCondition: "live check passes"}},
	}
	if err := store.SaveCompletion(state); err != nil {
		t.Fatal(err)
	}
	res := tick(t, r)
	if len(d.killed) != 0 {
		t.Fatal("closed a worker with a pending obligation")
	}
	if !strings.Contains(strings.Join(CompletionLines(res.Completions), " "), "deployment pending (until live check passes)") {
		t.Fatalf("completion = %+v", res.Completions)
	}

	state, _, _ = store.Completion()
	session := state.Sessions["Linux/uf-1"]
	session.Obligations = nil
	state.Sessions["Linux/uf-1"] = session
	if err := store.SaveCompletion(state); err != nil {
		t.Fatal(err)
	}
	tick(t, r)
	if len(d.killed) != 1 {
		t.Fatalf("released worker was not closed: killed=%v", d.killed)
	}
}

func TestCloseFailureIsRetainedForRetry(t *testing.T) {
	r, d, gh, _, store := completionFixture(t)
	gh.state = GitHubState{MergedPRs: []MergedPR{{Number: 201, Head: "rw/190", HeadSHA: head("1")}}}
	d.killErr = errors.New("lifecycle unavailable")
	res := tick(t, r)
	if len(d.killed) != 0 {
		t.Fatal("recorded a failed close as completed")
	}
	lines := strings.Join(CompletionLines(res.Completions), " ")
	if !strings.Contains(lines, "close failed: lifecycle unavailable") {
		t.Fatalf("completion = %q", lines)
	}
	state, _, _ := store.Completion()
	if action := state.Sessions["Linux/uf-1"].Actions["close"]; action.Status != CompletionActionPending || action.Attempts != 1 {
		t.Fatalf("close action = %+v", action)
	}

	d.killErr = nil
	tick(t, r)
	if len(d.killed) != 1 {
		t.Fatalf("retry did not close: %v", d.killed)
	}
	state, _, _ = store.Completion()
	if action := state.Sessions["Linux/uf-1"].Actions["close"]; action.Status != CompletionActionCompleted {
		t.Fatalf("close action after retry = %+v", action)
	}
}

func TestPausedCampaignNeverMergesOrCloses(t *testing.T) {
	r, d, gh, fm, store := completionFixture(t)
	gh.state = GitHubState{MergedPRs: []MergedPR{{Number: 201, Head: "rw/190", HeadSHA: head("1")}}}
	if err := store.Pause("campaign stopped by user"); err != nil {
		t.Fatal(err)
	}
	res := tick(t, r)
	if res.Outcome != OutcomePaused || fm.calls() != 0 || len(d.killed) != 0 {
		t.Fatalf("result = %+v mergeCalls=%d killed=%v", res, fm.calls(), d.killed)
	}
}

func TestHostQualifiedObligationDoesNotBlockAnotherHost(t *testing.T) {
	r, d, gh, _, store := completionFixture(t)
	gh.state = GitHubState{MergedPRs: []MergedPR{{Number: 201, Head: "rw/190", HeadSHA: head("1")}}}
	state, _, _ := store.Completion()
	state.Sessions["Linux/uf-1"] = SessionCompletion{
		Host: "Linux", Session: "uf-1",
		Obligations: []Obligation{{ID: "deploy", Reason: "deployment pending"}},
	}
	if err := store.SaveCompletion(state); err != nil {
		t.Fatal(err)
	}
	// The same session id on another host is a different identity.
	d.workers["IMac"] = []Worker{cleanWorker("rw/190", 201)}
	r.Config.Hosts = []string{"Linux", "IMac"}
	tick(t, r)
	if len(d.killed) != 1 || d.killed[0] != "IMac/uf-1" {
		t.Fatalf("killed = %v; only the host without the obligation may close", d.killed)
	}
}

func TestConfigAuthorizationFlagsAreDurableOptIns(t *testing.T) {
	cfg, err := ConfigFromEnv(func(key string) string {
		if key == "RW_TICK_MERGE_AUTHORIZED" || key == "RW_TICK_CLOSE_COMPLETED" {
			return "1"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.MergeAuthorized || !cfg.CloseCompleted {
		t.Fatalf("config = %+v", cfg)
	}
	if cfg.HoldLabels[0] != "needs-human" {
		t.Fatalf("hold labels = %v", cfg.HoldLabels)
	}
}

func TestMergeBlockersAreExact(t *testing.T) {
	cfg := DefaultConfig()
	base := readyPR(1, "rw/1", head("a"))
	base.AOReviewStatus = "up_to_date"
	cases := map[string]struct {
		mutate func(*PullRequest)
		want   string
	}{
		"head unknown":          {func(pr *PullRequest) { pr.HeadSHA = "" }, "head SHA is unknown"},
		"draft":                 {func(pr *PullRequest) { pr.Draft = true }, "pull request is a draft"},
		"failing checks":        {func(pr *PullRequest) { pr.Checks = []Check{{Result: "FAILURE"}} }, "required checks are red"},
		"no AO native approval": {func(pr *PullRequest) { pr.AOReviewStatus = "" }, "no current-head AO native approval"},
		"AO up to date":         {func(pr *PullRequest) { pr.ReviewDecision = ""; pr.AOReviewStatus = "up_to_date" }, ""},
		"AO changes requested":  {func(pr *PullRequest) { pr.AOReviewStatus = "changes_requested" }, "AO review is changes_requested"},
		"AO running":            {func(pr *PullRequest) { pr.AOReviewStatus = "running" }, "AO review is running"},
		"human wins over AO":    {func(pr *PullRequest) { pr.AOReviewStatus = "up_to_date"; pr.ReviewDecision = "CHANGES_REQUESTED" }, "review changes requested"},
		"changes requested":     {func(pr *PullRequest) { pr.ReviewDecision = "CHANGES_REQUESTED" }, "review changes requested"},
		"unresolved thread":     {func(pr *PullRequest) { pr.Threads = []ReviewThread{{Body: "fix this"}} }, "1 unresolved review thread(s)"},
		"conflicting":           {func(pr *PullRequest) { pr.Mergeable = "CONFLICTING" }, "mergeability is CONFLICTING"},
		"hold label":            {func(pr *PullRequest) { pr.Labels = []string{"status:blocked"} }, "hold label status:blocked"},
		"resolved thread":       {func(pr *PullRequest) { pr.Threads = []ReviewThread{{Resolved: true, Body: "old"}} }, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pr := base
			pr.Checks = append([]Check(nil), base.Checks...)
			pr.Labels = nil
			pr.Threads = nil
			tc.mutate(&pr)
			got := strings.Join(MergeBlockers(cfg, pr, now), ", ")
			if tc.want == "" {
				if got != "" {
					t.Fatalf("blockers = %q, want none", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("blockers = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestApplyWorkerReviewRequiresTheExactHead(t *testing.T) {
	pr := readyPR(201, "rw/190", head("new"))
	worker := cleanWorker("rw/190", 201)
	worker.Reviews = []WorkerReview{{PRNumber: 201, TargetSHA: head("old"), Status: "up_to_date"}}
	got := applyWorkerReview(worker, pr)
	if got.AOReviewStatus != "" {
		t.Fatalf("stale AO verdict applied to a new head: %q", got.AOReviewStatus)
	}
	if blockers := strings.Join(MergeBlockers(DefaultConfig(), got, now), ", "); !strings.Contains(blockers, "no current-head AO native approval") {
		t.Fatalf("blockers = %q", blockers)
	}
}

func TestCloseAuthorizationIsExplicit(t *testing.T) {
	r, d, gh, _, _ := completionFixture(t)
	r.Config.CloseCompleted = false
	gh.state = GitHubState{MergedPRs: []MergedPR{{Number: 201, Head: "rw/190", HeadSHA: head("1")}}}
	res := tick(t, r)
	if len(d.killed) != 0 {
		t.Fatal("closed without authorization")
	}
	if !strings.Contains(strings.Join(CompletionLines(res.Completions), " "), "completion closure not authorized") {
		t.Fatalf("completion = %+v", res.Completions)
	}
}

func TestCompletedActionsSurviveARestart(t *testing.T) {
	r, d, gh, fm, store := completionFixture(t)
	tick(t, r)
	gh.state = GitHubState{MergedPRs: []MergedPR{{Number: 201, Head: "rw/190", HeadSHA: head("1")}}}
	tick(t, r)
	if len(d.killed) != 1 || fm.calls() != 1 {
		t.Fatalf("before restart: merges=%d killed=%v", fm.calls(), d.killed)
	}

	// A new process reads the same atomic state files. It must not repeat the
	// completed merge or close side effects.
	restartedDaemon := &fakeDaemon{
		status:        d.status,
		orchestrators: d.orchestrators,
		sessions:      map[string]SessionState{"uf-orchestrator-7": {Status: "idle", Activity: "idle"}},
		workers:       map[string][]Worker{"Linux": {cleanWorker("rw/190", 201)}},
	}
	restarted := &Runner{
		Config: r.Config, Daemon: restartedDaemon, GitHub: gh, Merger: fm,
		Store: store, Now: func() time.Time { return now.Add(time.Minute) },
	}
	tick(t, restarted)
	if fm.calls() != 1 || len(restartedDaemon.killed) != 0 {
		t.Fatalf("restart replayed completed work: merges=%d killed=%v", fm.calls(), restartedDaemon.killed)
	}
}

func TestAOCurrentHeadApprovalAuthorizesMerge(t *testing.T) {
	r, _, gh, fm, _ := completionFixture(t)
	pr := readyPR(201, "rw/190", head("1"))
	pr.ReviewDecision = ""
	gh.state.OpenPRs = []PullRequest{pr}
	worker := r.Daemon.(*fakeDaemon).workers["Linux"][0]
	worker.Reviews = []WorkerReview{{PRNumber: 201, TargetSHA: head("1"), Status: "up_to_date"}}
	r.Daemon.(*fakeDaemon).workers["Linux"] = []Worker{worker}
	blk := MergeBlockers(r.Config, applyWorkerReview(worker, gh.state.OpenPRs[0]), now)
	tick(t, r)
	state, _, _ := r.Store.Completion()
	if fm.calls() != 1 {
		t.Fatalf("AO current-head approval did not enable the merge: calls=%d blockers=%v worker=%+v state=%+v", fm.calls(), blk, worker, state)
	}
}

func TestPendingCloseIsDroppedWhenAnObligationAppears(t *testing.T) {
	r, d, gh, _, store := completionFixture(t)
	gh.state = GitHubState{MergedPRs: []MergedPR{{Number: 201, Head: "rw/190", HeadSHA: head("1")}}}
	d.killErr = errors.New("lifecycle unavailable")
	tick(t, r)
	state, _, _ := store.Completion()
	session := state.Sessions["Linux/uf-1"]
	if action := session.Actions["close"]; action.Status != CompletionActionPending {
		t.Fatalf("close action = %+v", action)
	}
	session.Obligations = []Obligation{{ID: "deploy", Reason: "deployment pending"}}
	state.Sessions["Linux/uf-1"] = session
	if err := store.SaveCompletion(state); err != nil {
		t.Fatal(err)
	}
	d.killErr = nil
	tick(t, r)
	if len(d.killed) != 0 {
		t.Fatalf("pending close ignored the new obligation: %v", d.killed)
	}
	state, _, _ = store.Completion()
	if action := state.Sessions["Linux/uf-1"].Actions["close"]; action.Status != CompletionActionSuperseded {
		t.Fatalf("close action after obligation = %+v", action)
	}
}

func TestSupersededMergeActionStopsReporting(t *testing.T) {
	r, _, gh, fm, store := completionFixture(t)
	fm.err = ErrMergeHeadChanged
	tick(t, r)
	state, _, _ := store.Completion()
	var key string
	for actionKey, action := range state.Sessions["Linux/uf-1"].Actions {
		if strings.HasPrefix(actionKey, "merge:201@") {
			key = actionKey
			if action.Status != CompletionActionSuperseded {
				t.Fatalf("head-changed action = %+v", action)
			}
		}
	}
	if key == "" {
		t.Fatal("merge action not recorded")
	}
	// Change the head and keep it unapproved. The old action must not keep
	// producing an error in subsequent ticks.
	gh.state.OpenPRs[0].HeadSHA = head("2")
	gh.state.OpenPRs[0].ReviewDecision = ""
	fm.err = nil
	res := tick(t, r)
	if strings.Contains(strings.Join(CompletionLines(res.Completions), " "), "head changed") {
		t.Fatalf("superseded action still reported: %+v", res.Completions)
	}
	if fm.calls() != 1 {
		t.Fatalf("superseded action was retried: calls=%d", fm.calls())
	}
}

func TestPendingMergeIsNotRunForAnAbsentSession(t *testing.T) {
	r, d, gh, fm, store := completionFixture(t)
	sha := head("1")
	state := CompletionState{Sessions: map[string]SessionCompletion{
		"Linux/uf-1": {
			Host: "Linux", Session: "uf-1",
			Actions: map[string]CompletionAction{
				"merge:201@" + sha: {Kind: ActionMerge, Status: CompletionActionPending, PR: 201, HeadSHA: sha},
			},
		},
	}}
	if err := store.SaveCompletion(state); err != nil {
		t.Fatal(err)
	}
	d.workers["Linux"] = nil
	gh.state.OpenPRs = []PullRequest{readyPR(201, "rw/190", sha)}
	tick(t, r)
	if fm.calls() != 0 {
		t.Fatalf("merged a PR for a session that is no longer present: calls=%d", fm.calls())
	}
	got, _, _ := store.Completion()
	if action := got.Sessions["Linux/uf-1"].Actions["merge:201@"+sha]; action.Status != CompletionActionSuperseded {
		t.Fatalf("action = %+v", action)
	}
}

func TestAOReviewChangeRequestBlocksAnExistingMergeAction(t *testing.T) {
	r, d, gh, fm, _ := completionFixture(t)
	sha := head("1")
	pr := readyPR(201, "rw/190", sha)
	pr.ReviewDecision = ""
	gh.state.OpenPRs = []PullRequest{pr}
	worker := d.workers["Linux"][0]
	worker.Reviews = []WorkerReview{{PRNumber: 201, TargetSHA: sha, Status: "up_to_date"}}
	d.workers["Linux"] = []Worker{worker}

	state, _, _ := r.Store.Completion()
	state.Sessions["Linux/uf-1"] = SessionCompletion{
		Host: "Linux", Session: "uf-1",
		Actions: map[string]CompletionAction{
			"merge:201@" + sha: {Kind: ActionMerge, Status: CompletionActionPending, PR: 201, HeadSHA: sha, AOReviewStatus: "up_to_date"},
		},
	}
	if err := r.Store.SaveCompletion(state); err != nil {
		t.Fatal(err)
	}
	worker.Reviews = []WorkerReview{{PRNumber: 201, TargetSHA: sha, Status: "changes_requested"}}
	d.workers["Linux"] = []Worker{worker}
	tick(t, r)
	if fm.calls() != 0 {
		t.Fatal("merged after AO requested changes")
	}
}

func TestSkippedDraftPhaseChecksBlockMergeUntilNewRunsFinish(t *testing.T) {
	cfg := DefaultConfig()
	pr := readyPR(1, "rw/1", head("a"))
	pr.AOReviewStatus = "up_to_date"
	for name, tc := range map[string]struct {
		checks []Check
		want   string
	}{
		"draft-phase skips only": {[]Check{{Result: "SKIPPED"}, {Result: "SKIPPED"}}, "required checks are pending"},
		"new runs queued":        {[]Check{{Result: "QUEUED"}, {Result: "QUEUED"}}, "required checks are pending"},
		"new runs finished":      {[]Check{{Result: "SUCCESS"}, {Result: "SUCCESS"}}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			pr := pr
			pr.Draft, pr.Checks = false, tc.checks
			got := strings.Join(MergeBlockers(cfg, pr, now), ", ")
			if got != tc.want {
				t.Fatalf("blockers = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDraftPhaseSkipsHoldMergeForTenMinutesAfterReady(t *testing.T) {
	cfg := DefaultConfig()
	flip := now.Add(-time.Minute)
	pr := readyPR(1, "rw/1", head("a"))
	pr.AOReviewStatus, pr.ReadyAt = "up_to_date", flip
	before, after := flip.Add(-time.Minute), flip.Add(time.Second)
	cheapOnly := []Check{{Result: "SUCCESS", CompletedAt: after}, {Result: "SKIPPED", CompletedAt: before}}
	for name, tc := range map[string]struct {
		checks []Check
		at     time.Time
		want   string
	}{
		"cheap success after flip, expensive unregistered": {cheapOnly, now, "required checks are pending"},
		"same state after the window":                      {cheapOnly, flip.Add(staleSkipWindow), ""},
		"just inside the window":                           {cheapOnly, flip.Add(staleSkipWindow - time.Second), "required checks are pending"},
		"new run registered":                               {[]Check{{Result: "SUCCESS", CompletedAt: after}, {Result: "QUEUED"}}, now, "required checks are pending"},
		"new runs finished":                                {[]Check{{Result: "SUCCESS", CompletedAt: after}, {Result: "SUCCESS", CompletedAt: after}}, now, ""},
		"skip after flip is genuine":                       {[]Check{{Result: "SUCCESS", CompletedAt: before}, {Result: "SKIPPED", CompletedAt: after}}, now, ""},
	} {
		t.Run(name, func(t *testing.T) {
			pr := pr
			pr.Checks = tc.checks
			if got := strings.Join(MergeBlockers(cfg, pr, tc.at), ", "); got != tc.want {
				t.Fatalf("blockers = %q, want %q", got, tc.want)
			}
		})
	}
}
