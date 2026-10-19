package review

import (
	stdctx "context"
	"fmt"
	"log/slog"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// A running review pass is bounded: a stalled reviewer must not keep its run
// running forever with no verdict and no retry.
const (
	// reviewRunInactivityTimeout is the longest a pass may go without progress.
	// Progress is the reviewer row's UpdatedAt, advanced by hooks and launches.
	reviewRunInactivityTimeout = 30 * time.Minute
	// reviewRunMaxLifetime bounds a pass even while it keeps acknowledging work.
	reviewRunMaxLifetime = 2 * time.Hour
	// reviewRunTimeoutCancelTimeout bounds the best-effort pane interruption.
	reviewRunTimeoutCancelTimeout = 5 * time.Second
)

// reviewRunTimeoutReason returns the failure body past a pass's bound.
// lastProgress is its start or a later acknowledgement; zero means unknown.
func reviewRunTimeoutReason(run domain.ReviewRun, lastProgress, now time.Time) string {
	if lastProgress.IsZero() {
		return ""
	}
	if lastProgress.After(now) {
		lastProgress = now
	}
	if now.Sub(lastProgress) >= reviewRunInactivityTimeout {
		return fmt.Sprintf("review timed out: no progress for %s", reviewRunInactivityTimeout)
	}
	if !run.CreatedAt.IsZero() && now.Sub(run.CreatedAt) >= reviewRunMaxLifetime {
		return fmt.Sprintf("review timed out: exceeded the %s lifetime", reviewRunMaxLifetime)
	}
	return ""
}

// timeoutRunningRuns fails passes past their bound and stops their reviewer,
// so the durable state stops reporting running. Read and trigger paths call it.
func (e *Engine) timeoutRunningRuns(
	ctx stdctx.Context,
	workerID domain.SessionID,
	runs []domain.ReviewRun,
	reviews []domain.Review,
) (bool, error) {
	now := e.clock()
	reviewByID := make(map[string]domain.Review, len(reviews))
	for _, review := range reviews {
		reviewByID[review.ID] = review
	}
	stopped := make(map[string]bool)
	changed := false
	var timedOut []domain.ReviewRun
	for _, run := range runs {
		if run.Status != domain.ReviewRunRunning || run.Verdict != domain.VerdictNone {
			continue
		}
		review := reviewByID[run.ReviewID]
		lastProgress := run.CreatedAt
		if review.UpdatedAt.After(lastProgress) {
			lastProgress = review.UpdatedAt
		}
		reason := reviewRunTimeoutReason(run, lastProgress, now)
		if reason == "" {
			continue
		}
		e.stopTimedOutReviewer(ctx, review, stopped)
		updated, err := e.store.UpdateReviewRunResult(
			ctx, run.ID, domain.ReviewRunFailed, domain.VerdictNone,
			reason, "", run.AutoInjectReview,
		)
		if err != nil {
			return changed, fmt.Errorf("fail timed-out review run %q: %w", run.ID, err)
		}
		if !updated {
			// A concurrent submit or cancellation already made it terminal.
			continue
		}
		slog.Default().WarnContext(ctx, "review run timed out",
			"session", workerID, "run", run.ID, "harness", run.Harness, "reason", reason)
		run.Status = domain.ReviewRunFailed
		run.Verdict = domain.VerdictNone
		run.Body = reason
		timedOut = append(timedOut, run)
		changed = true
	}
	if len(timedOut) > 0 {
		e.releaseFinishedCheckouts(ctx, workerID, timedOut)
	}
	return changed, nil
}

// stopTimedOutReviewer interrupts a timed-out pass's pane once per reviewer.
// Cancellation is best effort; chat reviewers have no pane handle here.
func (e *Engine) stopTimedOutReviewer(ctx stdctx.Context, review domain.Review, stopped map[string]bool) {
	if review.ID == "" || review.ReviewerHandleID == "" || stopped[review.ID] {
		return
	}
	stopped[review.ID] = true
	cancelCtx, cancel := stdctx.WithTimeout(stdctx.WithoutCancel(ctx), reviewRunTimeoutCancelTimeout)
	defer cancel()
	if err := e.launcher.Cancel(cancelCtx, review.ReviewerHandleID, review.Harness); err != nil {
		slog.Default().WarnContext(ctx, "review watchdog: cancel timed-out reviewer failed",
			"review", review.ID, "harness", review.Harness, "err", err)
	}
}
