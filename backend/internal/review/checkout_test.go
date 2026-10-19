package review

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// --- git fixture ---

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func initGitRepo(t *testing.T, dir, file, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	return commitFile(t, dir, file, content)
}

func commitFile(t *testing.T, dir, file, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", content)
	return runGit(t, dir, "rev-parse", "HEAD")
}

// --- fake manager for engine tests ---

type fakeCheckout struct {
	prepared PreparedCheckout
	err      error
	requests []CheckoutRequest
	released []string
}

func (f *fakeCheckout) Prepare(_ context.Context, req CheckoutRequest) (PreparedCheckout, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return PreparedCheckout{}, f.err
	}
	out := f.prepared
	out.Key = req.Key
	if out.SHA == "" {
		out.SHA = req.TargetSHA
	}
	if out.Path == "" {
		out.Path = req.WorkspacePath
	}
	if out.Tree == "" {
		out.Tree = domain.ReviewCheckoutWorker
	}
	return out, nil
}

func (f *fakeCheckout) Release(_ context.Context, key string) error {
	f.released = append(f.released, key)
	return nil
}

func (f *fakeCheckout) Reopen(_ context.Context, _ string) (PreparedCheckout, error) {
	if f.err != nil {
		return PreparedCheckout{}, f.err
	}
	return f.prepared, nil
}

// --- GitCheckout tests ---

func TestGitCheckoutUsesCleanWorkerCheckoutAtTarget(t *testing.T) {
	dataDir := t.TempDir()
	origin := filepath.Join(t.TempDir(), "origin")
	sha := initGitRepo(t, origin, "file.txt", "one")
	workspace := filepath.Join(t.TempDir(), "workspace")
	runGit(t, t.TempDir(), "clone", origin, workspace)

	prepared, err := NewGitCheckout(dataDir).Prepare(t.Context(), CheckoutRequest{
		Key: "batch-1", WorkspacePath: workspace, TargetSHA: sha,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if prepared.Tree != domain.ReviewCheckoutWorker || prepared.Disposable || prepared.Path != workspace || prepared.SHA != sha {
		t.Fatalf("prepared = %+v, want the clean worker checkout at %s", prepared, sha)
	}
	if status := runGit(t, workspace, "status", "--porcelain"); status != "" {
		t.Fatalf("verification dirtied the worker checkout: %q", status)
	}
}

func TestGitCheckoutMaterializesDisposableWorktreeForDirtyTree(t *testing.T) {
	dataDir := t.TempDir()
	origin := filepath.Join(t.TempDir(), "origin")
	sha := initGitRepo(t, origin, "file.txt", "one")
	workspace := filepath.Join(t.TempDir(), "workspace")
	runGit(t, t.TempDir(), "clone", origin, workspace)
	if err := os.WriteFile(filepath.Join(workspace, "scratch.txt"), []byte("uncommitted"), 0o600); err != nil {
		t.Fatal(err)
	}

	manager := NewGitCheckout(dataDir)
	prepared, err := manager.Prepare(t.Context(), CheckoutRequest{
		Key: "batch-2", WorkspacePath: workspace, TargetSHA: sha,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !prepared.Disposable || prepared.Tree != domain.ReviewCheckoutDisposable {
		t.Fatalf("prepared = %+v, want a disposable worktree", prepared)
	}
	if prepared.Path == workspace {
		t.Fatal("dirty worker checkout must not be reviewed in place")
	}
	if got := runGit(t, prepared.Path, "rev-parse", "HEAD"); got != sha {
		t.Fatalf("disposable HEAD = %s, want %s", got, sha)
	}
	if status := runGit(t, prepared.Path, "status", "--porcelain"); status != "" {
		t.Fatalf("disposable worktree is not clean: %q", status)
	}
	if status := runGit(t, workspace, "status", "--porcelain"); !strings.Contains(status, "scratch.txt") {
		t.Fatalf("worker checkout changed: %q", status)
	}

	if err := manager.Release(t.Context(), "batch-2"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := os.Stat(prepared.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disposable worktree still exists after release: %v", err)
	}
	if err := manager.Release(t.Context(), "batch-2"); err != nil {
		t.Fatalf("Release must be idempotent: %v", err)
	}
}

func TestGitCheckoutFetchesTargetCommitForDisposableWorktree(t *testing.T) {
	dataDir := t.TempDir()
	origin := filepath.Join(t.TempDir(), "origin")
	first := initGitRepo(t, origin, "file.txt", "one")
	workspace := filepath.Join(t.TempDir(), "workspace")
	runGit(t, t.TempDir(), "clone", origin, workspace)
	second := commitFile(t, origin, "file.txt", "two")

	prepared, err := NewGitCheckout(dataDir).Prepare(t.Context(), CheckoutRequest{
		Key: "batch-3", WorkspacePath: workspace, TargetSHA: second, PRURL: "https://github.com/o/r/pull/7",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !prepared.Disposable {
		t.Fatalf("prepared = %+v, want a disposable worktree", prepared)
	}
	if got := runGit(t, prepared.Path, "rev-parse", "HEAD"); got != second {
		t.Fatalf("disposable HEAD = %s, want %s", got, second)
	}
	if got := runGit(t, workspace, "rev-parse", "HEAD"); got != first {
		t.Fatalf("worker checkout moved to %s, want %s", got, first)
	}
}

func TestGitCheckoutFailsWithMismatchWhenTargetIsUnavailable(t *testing.T) {
	dataDir := t.TempDir()
	origin := filepath.Join(t.TempDir(), "origin")
	sha := initGitRepo(t, origin, "file.txt", "one")
	workspace := filepath.Join(t.TempDir(), "workspace")
	runGit(t, t.TempDir(), "clone", origin, workspace)
	target := strings.Repeat("a", 40)

	_, err := NewGitCheckout(dataDir).Prepare(t.Context(), CheckoutRequest{
		Key: "batch-4", WorkspacePath: workspace, TargetSHA: target,
	})
	if err == nil {
		t.Fatal("Prepare succeeded for an unavailable target commit")
	}
	for _, want := range []string{"review checkout mismatch", shortCheckoutSHA(sha), shortCheckoutSHA(target)} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %q, want %q", err, want)
		}
	}
}

func TestGitCheckoutRequiresTargetSHA(t *testing.T) {
	_, err := NewGitCheckout(t.TempDir()).Prepare(t.Context(), CheckoutRequest{
		Key: "batch-5", WorkspacePath: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "target SHA") {
		t.Fatalf("err = %v, want a missing target SHA error", err)
	}
}

func TestPullHeadRef(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want string
	}{
		{url: "https://github.com/o/r/pull/42", want: "refs/pull/42/head"},
		{url: "https://api.github.com/repos/o/r/pulls/42", want: "refs/pull/42/head"},
		{url: "https://gitlab.com/o/r/-/merge_requests/9", want: "refs/merge-requests/9/head"},
		{url: "https://github.com/o/r", want: ""},
		{url: "", want: ""},
	} {
		if got := pullHeadRef(tc.url); got != tc.want {
			t.Fatalf("pullHeadRef(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

// --- engine integration ---

func TestTriggerUsesVerifiedCheckoutAndRecordsReviewedTree(t *testing.T) {
	store := &fakeStore{}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	checkout := &fakeCheckout{prepared: PreparedCheckout{
		Path:       "/tmp/review-worktree",
		SHA:        "sha1",
		Tree:       domain.ReviewCheckoutDisposable,
		Disposable: true,
	}}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)
	eng.checkout = checkout

	res, err := trigger(context.Background(), eng, "mer-1", "", domain.AgentConfig{})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if !res.Created || launcher.spawnCount != 1 {
		t.Fatalf("result=%+v spawns=%d", res, launcher.spawnCount)
	}
	if len(checkout.requests) != 1 || checkout.requests[0].TargetSHA != "sha1" || checkout.requests[0].WorkspacePath != liveWorker().Metadata.WorkspacePath {
		t.Fatalf("checkout requests = %+v", checkout.requests)
	}
	spec := launcher.specs[0]
	if spec.WorkspacePath != "/tmp/review-worktree" || spec.CheckoutSHA != "sha1" || spec.CheckoutTree != domain.ReviewCheckoutDisposable {
		t.Fatalf("launch spec checkout = %+v", spec)
	}
	if len(store.runs) != 1 || store.runs[0].ReviewedSHA != "sha1" || store.runs[0].ReviewedTree != domain.ReviewCheckoutDisposable {
		t.Fatalf("recorded run checkout = %+v", store.runs)
	}
	if store.runs[0].ReviewedCheckoutKey != checkout.requests[0].Key || store.runs[0].ReviewedCheckoutKey == "" {
		t.Fatalf("recorded checkout key = %q, want %q", store.runs[0].ReviewedCheckoutKey, checkout.requests[0].Key)
	}
	if len(checkout.released) != 0 {
		t.Fatalf("a running pass must keep its checkout: released=%v", checkout.released)
	}
}

func TestTriggerReleasesDisposableCheckoutWhenLaunchFails(t *testing.T) {
	store := &fakeStore{}
	launcher := &fakeLauncher{handle: "review-mer-1", spawnErr: errors.New("tmux failed")}
	checkout := &fakeCheckout{prepared: PreparedCheckout{
		Path:       "/tmp/review-worktree",
		SHA:        "sha1",
		Tree:       domain.ReviewCheckoutDisposable,
		Disposable: true,
	}}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)
	eng.checkout = checkout

	if _, err := trigger(context.Background(), eng, "mer-1", "", domain.AgentConfig{}); err == nil {
		t.Fatal("Trigger succeeded despite a launch failure")
	}
	if len(checkout.released) != 1 {
		t.Fatalf("released = %v, want the failed pass's disposable checkout", checkout.released)
	}
}

func TestRerunPreparesCheckoutAtTheRequestedRunTarget(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerClaudeCode, ReviewerHandleID: "review-mer-1", AgentSessionID: "native-reviewer-1"},
		runs: []domain.ReviewRun{{
			ID: "approved", ReviewID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerClaudeCode,
			PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1", Status: domain.ReviewRunComplete, Verdict: domain.VerdictApproved,
		}},
	}
	launcher := &fakeLauncher{alive: true, handle: "review-mer-1"}
	checkout := &fakeCheckout{prepared: PreparedCheckout{Path: "/ws/mer-1", Tree: domain.ReviewCheckoutWorker}}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)
	eng.checkout = checkout

	if _, err := eng.TriggerWithOptions(context.Background(), "mer-1", TriggerOptions{Rerun: true}); err != nil {
		t.Fatalf("TriggerWithOptions: %v", err)
	}
	if len(checkout.requests) != 1 || checkout.requests[0].TargetSHA != "sha1" {
		t.Fatalf("checkout requests = %+v, want the rerun's target commit sha1", checkout.requests)
	}
}

func TestRestoreCheckoutReVerifiesMovedWorkerCheckout(t *testing.T) {
	dataDir := t.TempDir()
	origin := filepath.Join(t.TempDir(), "origin")
	first := initGitRepo(t, origin, "file.txt", "one")
	workspace := filepath.Join(t.TempDir(), "workspace")
	runGit(t, t.TempDir(), "clone", origin, workspace)
	second := commitFile(t, origin, "file.txt", "two")
	runGit(t, workspace, "fetch", "origin")
	runGit(t, workspace, "checkout", "--detach", second)

	worker := liveWorker()
	worker.Metadata.WorkspacePath = workspace
	manager := NewGitCheckout(dataDir)
	eng := &Engine{checkout: manager}
	prepared, ok, err := eng.restoreCheckout(context.Background(), worker, []domain.ReviewRun{{
		ID: "run-1", BatchID: "batch-1", Status: domain.ReviewRunRunning,
		TargetSHA: first, ReviewedTree: domain.ReviewCheckoutWorker,
	}})
	if err != nil || !ok {
		t.Fatalf("restoreCheckout ok=%v err=%v", ok, err)
	}
	if !prepared.Disposable || prepared.Path == workspace {
		t.Fatalf("prepared = %+v, want a disposable worktree because the worker moved", prepared)
	}
	if got := runGit(t, prepared.Path, "rev-parse", "HEAD"); got != first {
		t.Fatalf("restored checkout HEAD = %s, want %s", got, first)
	}
	if err := manager.Release(context.Background(), prepared.Key); err != nil {
		t.Fatalf("Release: %v", err)
	}
}

func TestTriggerSurfacesCheckoutIOErrorWithoutInvalid(t *testing.T) {
	store := &fakeStore{}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)
	eng.checkout = &fakeCheckout{err: errors.New("git fetch: connection reset")}

	_, err := trigger(context.Background(), eng, "mer-1", "", domain.AgentConfig{})
	if err == nil {
		t.Fatal("Trigger succeeded despite a checkout I/O error")
	}
	if errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want an internal checkout error, not ErrInvalid", err)
	}
	if !strings.Contains(err.Error(), "prepare review checkout") {
		t.Fatalf("err = %v, want the checkout preparation context", err)
	}
}

func TestRestoreCheckoutReopensDisposableWorktree(t *testing.T) {
	checkout := &fakeCheckout{prepared: PreparedCheckout{
		Path: "/tmp/review-worktree", SHA: "sha1", Tree: domain.ReviewCheckoutDisposable, Disposable: true,
	}}
	eng := &Engine{checkout: checkout}
	prepared, ok, err := eng.restoreCheckout(context.Background(), liveWorker(), []domain.ReviewRun{{
		ID: "run-1", BatchID: "batch-1", Status: domain.ReviewRunRunning,
		TargetSHA: "sha1", ReviewedTree: domain.ReviewCheckoutDisposable,
	}})
	if err != nil || !ok {
		t.Fatalf("restoreCheckout ok=%v err=%v", ok, err)
	}
	if prepared.Path != "/tmp/review-worktree" || prepared.SHA != "sha1" || !prepared.Disposable {
		t.Fatalf("prepared = %+v, want the reopened disposable worktree", prepared)
	}
}

func TestRestoreCheckoutUsesWorkerTreeForVerifiedRun(t *testing.T) {
	eng := &Engine{checkout: &fakeCheckout{}}
	prepared, ok, err := eng.restoreCheckout(context.Background(), liveWorker(), []domain.ReviewRun{{
		ID: "run-1", BatchID: "batch-1", Status: domain.ReviewRunRunning,
		TargetSHA: "sha1", ReviewedTree: domain.ReviewCheckoutWorker,
	}})
	if err != nil || !ok {
		t.Fatalf("restoreCheckout ok=%v err=%v", ok, err)
	}
	if prepared.Path != liveWorker().Metadata.WorkspacePath || prepared.Tree != domain.ReviewCheckoutWorker {
		t.Fatalf("prepared = %+v, want the worker checkout", prepared)
	}
}

func TestTriggerRelaunchesReviewerAfterDisposableCheckout(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerClaudeCode, ReviewerHandleID: "review-mer-1", AgentSessionID: "native-reviewer-1"},
		runs: []domain.ReviewRun{{
			ID: "previous", ReviewID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerClaudeCode,
			PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1", Status: domain.ReviewRunComplete,
			Verdict: domain.VerdictApproved, ReviewedTree: domain.ReviewCheckoutDisposable,
		}},
	}
	launcher := &fakeLauncher{alive: true, handle: "review-mer-1"}
	checkout := &fakeCheckout{prepared: PreparedCheckout{Path: "/ws/mer-1", Tree: domain.ReviewCheckoutWorker}}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)
	eng.checkout = checkout

	if _, err := eng.TriggerWithOptions(context.Background(), "mer-1", TriggerOptions{Rerun: true}); err != nil {
		t.Fatalf("TriggerWithOptions: %v", err)
	}
	if launcher.spawnCount != 1 || launcher.notified {
		t.Fatalf("spawns=%d notified=%v, want a fresh launch over the verified checkout", launcher.spawnCount, launcher.notified)
	}
	if launcher.specs[0].WorkspacePath != "/ws/mer-1" {
		t.Fatalf("launch workspace = %q", launcher.specs[0].WorkspacePath)
	}
}

func TestTriggerSharesDisposableCheckoutAcrossSameHeadQueue(t *testing.T) {
	store := &fakeStore{}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	checkout := &fakeCheckout{prepared: PreparedCheckout{
		Path: "/tmp/review-worktree", Tree: domain.ReviewCheckoutDisposable, Disposable: true,
	}}
	prs := fakePRs{prs: []domain.PullRequest{
		{URL: "https://github.com/o/r/pull/1", Number: 1, HeadSHA: "same"},
		{URL: "https://github.com/o/r/pull/2", Number: 2, HeadSHA: "same"},
	}}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prs, fakeProjects{}, launcher)
	eng.checkout = checkout

	res, err := trigger(context.Background(), eng, "mer-1", "", domain.AgentConfig{})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if !res.Created || len(res.CreatedRuns) != 2 || launcher.spawnCount != 1 {
		t.Fatalf("result=%+v spawns=%d", res, launcher.spawnCount)
	}
	if len(checkout.requests) != 1 {
		t.Fatalf("checkout requests = %+v, want one shared checkout", checkout.requests)
	}
	key := checkout.requests[0].Key
	for _, run := range store.runs {
		if run.ReviewedCheckoutKey != key || run.ReviewedTree != domain.ReviewCheckoutDisposable {
			t.Fatalf("run checkout = %+v, want shared key %q", run, key)
		}
	}
	if launcher.specs[0].WorkspacePath != "/tmp/review-worktree" {
		t.Fatalf("launch workspace = %q", launcher.specs[0].WorkspacePath)
	}
}

func TestTriggerLeavesUnverifiedQueueRunsWithoutReviewedSHA(t *testing.T) {
	store := &fakeStore{}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	checkout := &fakeCheckout{prepared: PreparedCheckout{
		Path: "/ws/mer-1", SHA: "sha1", Tree: domain.ReviewCheckoutWorker,
	}}
	prs := fakePRs{prs: []domain.PullRequest{
		{URL: "https://github.com/o/r/pull/1", Number: 1, HeadSHA: "sha1"},
		{URL: "https://github.com/o/r/pull/2", Number: 2, HeadSHA: "sha2"},
	}}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prs, fakeProjects{}, launcher)
	eng.checkout = checkout

	res, err := trigger(context.Background(), eng, "mer-1", "", domain.AgentConfig{})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if !res.Created || len(store.runs) != 2 {
		t.Fatalf("runs = %+v", store.runs)
	}
	bySHA := make(map[string]domain.ReviewRun, len(store.runs))
	for _, run := range store.runs {
		bySHA[run.TargetSHA] = run
	}
	first, second := bySHA["sha1"], bySHA["sha2"]
	if first.ReviewedSHA != "sha1" || first.ReviewedTree != domain.ReviewCheckoutWorker {
		t.Fatalf("verified run = %+v, want sha1/worker_checkout", first)
	}
	if second.ReviewedSHA != "" || second.ReviewedTree != "" {
		t.Fatalf("unverified run = %+v, want empty reviewed SHA/tree", second)
	}
	if first.ReviewedCheckoutKey == "" || first.ReviewedCheckoutKey != second.ReviewedCheckoutKey {
		t.Fatalf("checkout keys = %q/%q, want one shared cleanup key", first.ReviewedCheckoutKey, second.ReviewedCheckoutKey)
	}
}

func TestTriggerRefusesDisposableCheckoutForMultipleHeads(t *testing.T) {
	store := &fakeStore{}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	checkout := &fakeCheckout{prepared: PreparedCheckout{
		Path:       "/tmp/review-worktree",
		Tree:       domain.ReviewCheckoutDisposable,
		Disposable: true,
	}}
	prs := fakePRs{prs: []domain.PullRequest{
		{URL: "https://github.com/o/r/pull/1", Number: 1, HeadSHA: "sha1"},
		{URL: "https://github.com/o/r/pull/2", Number: 2, HeadSHA: "sha2"},
	}}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prs, fakeProjects{}, launcher)
	eng.checkout = checkout

	_, err := trigger(context.Background(), eng, "mer-1", "", domain.AgentConfig{})
	if err == nil || !strings.Contains(err.Error(), "one disposable checkout cannot verify every head") {
		t.Fatalf("err = %v, want a multi-head checkout refusal", err)
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if launcher.spawnCount != 0 || launcher.notified {
		t.Fatalf("a refused checkout must not launch a reviewer: spawns=%d notified=%v", launcher.spawnCount, launcher.notified)
	}
	if len(checkout.released) != 1 {
		t.Fatalf("released = %v, want the unusable checkout removed", checkout.released)
	}
}

func TestTriggerFailsWhenCheckoutCannotBeVerified(t *testing.T) {
	store := &fakeStore{}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)
	eng.checkout = &fakeCheckout{err: fmt.Errorf("%w: worker checkout is at abc, target def", ErrCheckoutMismatch)}

	_, err := trigger(context.Background(), eng, "mer-1", "", domain.AgentConfig{})
	if err == nil || !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "worker checkout is at abc") {
		t.Fatalf("err = %v, want an invalid checkout mismatch", err)
	}
	if launcher.spawnCount != 0 {
		t.Fatalf("spawns = %d, want none", launcher.spawnCount)
	}
	if len(store.runs) != 1 || store.runs[0].Status != domain.ReviewRunFailed {
		t.Fatalf("runs = %+v, want the created run failed", store.runs)
	}
}
