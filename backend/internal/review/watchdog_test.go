package review

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func timedOutRunFixture(now time.Time) (domain.Review, domain.ReviewRun) {
	started := now.Add(-reviewRunInactivityTimeout - time.Minute)
	review := domain.Review{
		ID: "rev-1", SessionID: "mer-1", ProjectID: "mer", Harness: domain.ReviewerClaudeCode,
		ReviewerHandleID: "review-mer-1", ReviewerActivityState: domain.ActivityActive,
		CreatedAt: started, UpdatedAt: started,
	}
	run := domain.ReviewRun{
		ID: "run-1", ReviewID: review.ID, SessionID: review.SessionID, Harness: review.Harness,
		PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1",
		Status: domain.ReviewRunRunning, Verdict: domain.VerdictNone,
		CreatedAt: started, AutoInjectReview: true,
	}
	return review, run
}

func newEngineWithClock(store Store, launcher Launcher, clock func() time.Time) *Engine {
	ids := 0
	return New(Deps{
		Store: store, Sessions: fakeSessions{rec: liveWorker(), ok: true}, PRs: prAt("sha1"),
		Projects: fakeProjects{}, Launcher: launcher, Clock: clock,
		NewID: func() string { ids++; return "id-" + string(rune('0'+ids)) },
	})
}

func TestListNeverReportsRunningPastTheInactivityBound(t *testing.T) {
	now := time.Date(2026, 10, 19, 20, 40, 0, 0, time.UTC)
	review, run := timedOutRunFixture(now)
	store := &fakeStore{review: &review, runs: []domain.ReviewRun{run}}
	launcher := &fakeLauncher{handle: review.ReviewerHandleID, alive: true}
	eng := newEngineWithClock(store, launcher, func() time.Time { return now })

	state, err := eng.List(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(store.runs) != 1 || store.runs[0].Status != domain.ReviewRunFailed {
		t.Fatalf("run = %+v, want failed", store.runs)
	}
	if !strings.Contains(store.runs[0].Body, "timed out") {
		t.Fatalf("run body = %q, want a timeout reason", store.runs[0].Body)
	}
	if !launcher.cancelled || launcher.cancelledHandle != review.ReviewerHandleID {
		t.Fatalf("reviewer cancelled = %v handle %q, want %q", launcher.cancelled, launcher.cancelledHandle, review.ReviewerHandleID)
	}
	if len(state.Reviews) != 1 || state.Reviews[0].Status == ReviewStateRunning {
		t.Fatalf("reviews = %+v, want a status that no longer says running", state.Reviews)
	}
	if state.Reviews[0].Status != ReviewStateNeedsReview || state.Reviews[0].LatestRun == nil || state.Reviews[0].LatestRun.Body != store.runs[0].Body {
		t.Fatalf("reviews = %+v, want needs_review with the timeout reason", state.Reviews)
	}
}

func TestListKeepsAProgressingRunInsideTheInactivityBound(t *testing.T) {
	started := time.Date(2026, 10, 19, 19, 47, 0, 0, time.UTC)
	now := started.Add(reviewRunInactivityTimeout - time.Minute)
	review := domain.Review{
		ID: "rev-1", SessionID: "mer-1", ProjectID: "mer", Harness: domain.ReviewerClaudeCode,
		ReviewerHandleID: "review-mer-1", ReviewerActivityState: domain.ActivityActive,
		CreatedAt: started, UpdatedAt: now.Add(-5 * time.Minute),
	}
	run := domain.ReviewRun{
		ID: "run-1", ReviewID: review.ID, SessionID: review.SessionID, Harness: review.Harness,
		PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1",
		Status: domain.ReviewRunRunning, Verdict: domain.VerdictNone,
		CreatedAt: started, AutoInjectReview: true,
	}
	store := &fakeStore{review: &review, runs: []domain.ReviewRun{run}}
	launcher := &fakeLauncher{handle: review.ReviewerHandleID, alive: true}
	eng := newEngineWithClock(store, launcher, func() time.Time { return now })

	state, err := eng.List(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if store.runs[0].Status != domain.ReviewRunRunning || launcher.cancelled {
		t.Fatalf("run = %+v cancelled = %v, want a progressing run left alone", store.runs[0], launcher.cancelled)
	}
	if len(state.Reviews) != 1 || state.Reviews[0].Status != ReviewStateRunning {
		t.Fatalf("reviews = %+v, want running", state.Reviews)
	}

	// Progress stops: the same pass past the bound is failed on the next read.
	now = now.Add(reviewRunInactivityTimeout + time.Minute)
	if _, err := eng.List(context.Background(), run.SessionID); err != nil {
		t.Fatalf("List after stale progress: %v", err)
	}
	if store.runs[0].Status != domain.ReviewRunFailed || !launcher.cancelled {
		t.Fatalf("run = %+v cancelled = %v, want failed and cancelled", store.runs[0], launcher.cancelled)
	}
}

func TestListAppliesTheHardLifetimeCapToAChattyRun(t *testing.T) {
	now := time.Date(2026, 10, 19, 22, 0, 0, 0, time.UTC)
	review, run := timedOutRunFixture(now)
	run.CreatedAt = now.Add(-reviewRunMaxLifetime - time.Minute)
	review.CreatedAt = run.CreatedAt
	// The reviewer keeps acknowledging work, so inactivity alone never fires.
	review.UpdatedAt = now.Add(-time.Minute)
	store := &fakeStore{review: &review, runs: []domain.ReviewRun{run}}
	launcher := &fakeLauncher{handle: review.ReviewerHandleID, alive: true}
	eng := newEngineWithClock(store, launcher, func() time.Time { return now })

	state, err := eng.List(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if store.runs[0].Status != domain.ReviewRunFailed || !strings.Contains(store.runs[0].Body, "lifetime") {
		t.Fatalf("run = %+v, want the lifetime timeout reason", store.runs[0])
	}
	if state.Reviews[0].Status == ReviewStateRunning {
		t.Fatalf("reviews = %+v, want running to stop at the hard cap", state.Reviews)
	}
}

func TestTriggerRetriesAfterATimedOutRun(t *testing.T) {
	now := time.Date(2026, 10, 19, 21, 0, 0, 0, time.UTC)
	review, run := timedOutRunFixture(now)
	store := &fakeStore{review: &review, runs: []domain.ReviewRun{run}}
	launcher := &fakeLauncher{handle: review.ReviewerHandleID, alive: true}
	eng := newEngineWithClock(store, launcher, func() time.Time { return now })

	res, err := eng.TriggerWithOptions(context.Background(), run.SessionID, TriggerOptions{})
	if err != nil {
		t.Fatalf("TriggerWithOptions: %v", err)
	}
	if !res.Created {
		t.Fatalf("result = %+v, want a fresh pass after the timeout", res)
	}
	if store.runs[0].Status != domain.ReviewRunFailed || !strings.Contains(store.runs[0].Body, "timed out") {
		t.Fatalf("timed-out run = %+v, want it recorded as failed", store.runs[0])
	}
	if len(store.runs) != 2 || store.runs[1].Status != domain.ReviewRunRunning {
		t.Fatalf("runs = %+v, want a new running pass", store.runs)
	}
}

func TestReviewRunTimeoutReasonBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 19, 21, 0, 0, 0, time.UTC)
	cases := map[string]struct {
		created  time.Time
		progress time.Time
		want     string
	}{
		"inside inactivity":        {now.Add(-time.Hour), now.Add(-reviewRunInactivityTimeout + time.Second), ""},
		"at inactivity":            {now.Add(-time.Hour), now.Add(-reviewRunInactivityTimeout), "no progress"},
		"recent progress":          {now.Add(-time.Hour), now.Add(-time.Minute), ""},
		"hard lifetime":            {now.Add(-reviewRunMaxLifetime), now.Add(-time.Minute), "lifetime"},
		"future progress":          {now.Add(-time.Hour), now.Add(time.Minute), ""},
		"unknown progress is kept": {now.Add(-3 * time.Hour), time.Time{}, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := reviewRunTimeoutReason(domain.ReviewRun{CreatedAt: tc.created}, tc.progress, now)
			if tc.want == "" {
				if got != "" {
					t.Fatalf("reason = %q, want no timeout", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("reason = %q, want it to mention %q", got, tc.want)
			}
		})
	}
}
