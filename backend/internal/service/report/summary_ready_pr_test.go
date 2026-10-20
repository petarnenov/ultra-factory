package report_test

import (
	"context"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
)

const readyHead = "abc1234def"

// seedPR writes an open PR for worker at head.
func (f *summaryFixture) seedPR(worker domain.SessionID, number int, head string) domain.PullRequest {
	f.t.Helper()
	pr := domain.PullRequest{
		URL: "https://github.com/o/r/pull/" + string(rune('0'+number)), SessionID: worker, Number: number,
		HeadSHA: head, UpdatedAt: f.clock, Provider: "github", Host: "github.com", Repo: "o/r",
	}
	f.writePR(pr)
	return pr
}

func (f *summaryFixture) writePR(pr domain.PullRequest) {
	f.t.Helper()
	if err := f.store.WriteSCMObservation(context.Background(), pr, nil, nil, nil, nil, ports.ReviewWritePreserve); err != nil {
		f.t.Fatal(err)
	}
}

// seedRun records a review run for worker's PR.
func (f *summaryFixture) seedRun(id string, worker domain.SessionID, pr domain.PullRequest, sha string, status domain.ReviewRunStatus, verdict domain.ReviewVerdict) {
	f.t.Helper()
	ctx := context.Background()
	reviewID := "rev-" + string(worker)
	if err := f.store.UpsertReview(ctx, domain.Review{
		ID: reviewID, SessionID: worker, ProjectID: "p", Harness: domain.ReviewerClaudeCode, CreatedAt: f.clock, UpdatedAt: f.clock,
	}); err != nil {
		f.t.Fatal(err)
	}
	if err := f.store.InsertReviewRun(ctx, domain.ReviewRun{
		ID: id, ReviewID: reviewID, SessionID: worker, Harness: domain.ReviewerClaudeCode, PRURL: pr.URL,
		TargetSHA: sha, Status: status, Verdict: verdict, CreatedAt: f.clock,
	}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *summaryFixture) submitReady(coordinator domain.SessionID, key string, ready ...domain.ReadyPR) (domain.CoordinatorSummary, error) {
	rec, _, err := f.svc.Submit(context.Background(), reportsvc.SummaryInput{
		CoordinatorSessionID: coordinator, SubmissionKey: key, State: domain.SummaryDone, ReadyPRs: ready, Result: "done " + key,
	})
	return rec, err
}

func requireReadyPRInvalid(t *testing.T, err error, number int, reason string) {
	t.Helper()
	if apiCode(err) != "READY_PR_INVALID" {
		t.Fatalf("err = %v, want READY_PR_INVALID", err)
	}
	var apiErr *apierr.Error
	if !errorsAs(err, &apiErr) || apiErr.Details["number"] != number || apiErr.Details["reason"] != reason {
		t.Fatalf("details = %+v, want number %d reason %s", apiErr.Details, number, reason)
	}
}

func TestReadyPRAcceptedWithApprovedReviewOnTheCurrentHead(t *testing.T) {
	f := newSummaryFixture(t)
	pr := f.seedPR(f.workerA1.ID, 1, readyHead)
	f.seedRun("run-old", f.workerA1.ID, pr, "oldhead", domain.ReviewRunComplete, domain.VerdictApproved)
	f.seedRun("run-ok", f.workerA1.ID, pr, readyHead, domain.ReviewRunDelivered, domain.VerdictApproved)

	rec, err := f.submitReady(f.coordA.ID, "k1", domain.ReadyPR{Number: 1, HeadSHA: readyHead})
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.ReadyPR{{Number: 1, HeadSHA: readyHead, ReviewRunID: "run-ok"}}
	if len(rec.ReadyPRs) != 1 || rec.ReadyPRs[0] != want[0] {
		t.Fatalf("ready PRs = %+v, want %+v (run resolved)", rec.ReadyPRs, want)
	}
	if hist := f.history(f.coordA.ID); len(hist) != 1 || len(hist[0].ReadyPRs) != 1 || hist[0].ReadyPRs[0] != want[0] {
		t.Fatalf("history = %+v, want the stored ready PR", hist)
	}

	if _, err := f.submitReady(f.coordA.ID, "k2", domain.ReadyPR{Number: 1, HeadSHA: readyHead, ReviewRunID: "run-ok"}); err != nil {
		t.Fatalf("explicit run id: %v", err)
	}
}

func TestReadyPRRootReceivesPRsWithHeads(t *testing.T) {
	f := newSummaryFixture(t)
	pr := f.seedPR(f.workerA1.ID, 1, readyHead)
	f.seedRun("run-ok", f.workerA1.ID, pr, readyHead, domain.ReviewRunComplete, domain.VerdictApproved)
	if _, err := f.submitReady(f.coordA.ID, "k1", domain.ReadyPR{Number: 1, HeadSHA: readyHead}); err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(domain.CoordinatorSummaryBatchWindow)
	f.run()
	if len(f.delivery.submissions) != 1 {
		t.Fatalf("submissions = %#v, want one root turn", f.delivery.submissions)
	}
	if got := f.delivery.submissions[0].message; !strings.Contains(got, "ready PRs") || !strings.Contains(got, "#1 head "+readyHead+" (review run run-ok)") {
		t.Fatalf("message lacks the ready PR with its head:\n%s", got)
	}
}

func TestReadyPRRejectionStoresNothing(t *testing.T) {
	f := newSummaryFixture(t)
	owned := f.seedPR(f.workerA1.ID, 1, readyHead)
	f.seedRun("run-ok", f.workerA1.ID, owned, readyHead, domain.ReviewRunComplete, domain.VerdictApproved)
	notApproved := f.seedPR(f.workerA2.ID, 2, "head2")
	f.seedRun("run-changes", f.workerA2.ID, notApproved, "head2", domain.ReviewRunComplete, domain.VerdictChangesRequested)
	running := f.seedPR(f.workerA2.ID, 3, "head3")
	f.seedRun("run-running", f.workerA2.ID, running, "head3", domain.ReviewRunRunning, domain.VerdictNone)
	f.seedPR(f.workerA2.ID, 4, "head4")
	foreign := f.seedPR(f.workerB.ID, 5, "head5")
	f.seedRun("run-foreign", f.workerB.ID, foreign, "head5", domain.ReviewRunComplete, domain.VerdictApproved)
	f.seedPR(f.loose.ID, 6, "head6")
	closed := domain.PullRequest{
		URL: "https://github.com/o/r/pull/7", SessionID: f.workerA1.ID, Number: 7, HeadSHA: "head7", Merged: true,
		UpdatedAt: f.clock, Provider: "github", Host: "github.com", Repo: "o/r",
	}
	f.writePR(closed)
	f.seedRun("run-merged", f.workerA1.ID, closed, "head7", domain.ReviewRunComplete, domain.VerdictApproved)

	cases := []struct {
		name   string
		entry  domain.ReadyPR
		number int
		reason string
	}{
		{"stale head", domain.ReadyPR{Number: 1, HeadSHA: "movedhead"}, 1, "stale_head"},
		{"changes requested", domain.ReadyPR{Number: 2, HeadSHA: "head2"}, 2, "review_not_approved"},
		{"review still running", domain.ReadyPR{Number: 3, HeadSHA: "head3"}, 3, "review_not_approved"},
		{"no review at all", domain.ReadyPR{Number: 4, HeadSHA: "head4"}, 4, "review_not_approved"},
		{"another coordinator's worker", domain.ReadyPR{Number: 5, HeadSHA: "head5"}, 5, "pr_not_owned"},
		{"root-owned worker", domain.ReadyPR{Number: 6, HeadSHA: "head6"}, 6, "pr_not_owned"},
		{"unknown PR", domain.ReadyPR{Number: 99, HeadSHA: "x"}, 99, "pr_not_owned"},
		{"merged PR", domain.ReadyPR{Number: 7, HeadSHA: "head7"}, 7, "pr_not_open"},
		{"run of another PR", domain.ReadyPR{Number: 2, HeadSHA: "head2", ReviewRunID: "run-ok"}, 2, "review_run_not_found"},
		{"run reviewed another head", domain.ReadyPR{Number: 1, HeadSHA: readyHead, ReviewRunID: "run-changes"}, 1, "review_run_not_found"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.submitReady(f.coordA.ID, "bad-"+string(rune('a'+i)), domain.ReadyPR{Number: 1, HeadSHA: readyHead}, tc.entry)
			requireReadyPRInvalid(t, err, tc.number, tc.reason)
		})
	}
	if hist := f.history(f.coordA.ID); len(hist) != 0 {
		t.Fatalf("history = %+v, want nothing stored after rejections", hist)
	}
}

func TestReadyPRRejectsRunApprovedForAnotherHead(t *testing.T) {
	f := newSummaryFixture(t)
	pr := f.seedPR(f.workerA1.ID, 1, readyHead)
	f.seedRun("run-old", f.workerA1.ID, pr, "oldhead", domain.ReviewRunComplete, domain.VerdictApproved)
	_, err := f.submitReady(f.coordA.ID, "k1", domain.ReadyPR{Number: 1, HeadSHA: readyHead, ReviewRunID: "run-old"})
	requireReadyPRInvalid(t, err, 1, "review_head_mismatch")
}
