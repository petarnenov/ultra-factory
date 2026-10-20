package pr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// ciGateStore is the storage the CI gate needs beyond the action store.
type ciGateStore interface {
	GetSession(ctx context.Context, id domain.SessionID) (domain.SessionRecord, bool, error)
	ListAllSessions(ctx context.Context) ([]domain.SessionRecord, error)
	ListPRsBySession(ctx context.Context, id domain.SessionID) ([]domain.PullRequest, error)
	ListReviewRunsBySession(ctx context.Context, id domain.SessionID) ([]domain.ReviewRun, error)
	RecordSessionCIGateEvent(ctx context.Context, ev domain.SessionCIGateEvent) error
	ListSessionCIGateEvents(ctx context.Context, id domain.SessionID) ([]domain.SessionCIGateEvent, error)
}

// ReconcileCIGate sets each open PR's draft flag from the verdict on its head.
// Ungated sessions are skipped; calls are serialized to avoid double flips.
func (s *ActionService) ReconcileCIGate(ctx context.Context, sessionID domain.SessionID) error {
	s.ciGateMu.Lock()
	defer s.ciGateMu.Unlock()
	store, ok := s.store.(ciGateStore)
	if !ok || s.draft == nil || s.reader == nil {
		return fmt.Errorf("%w: no draft control is configured", ErrCIGateUnsupported)
	}
	session, found, err := store.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("load session %s: %w", sessionID, err)
	}
	if !found || !session.CIGateActive {
		return nil
	}
	prs, err := store.ListPRsBySession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("list pull requests: %w", err)
	}
	var open []domain.PullRequest
	for _, pr := range prs {
		if !pr.Merged && !pr.Closed && pr.HeadSHA != "" {
			open = append(open, pr)
		}
	}
	if len(open) == 0 {
		return nil
	}
	runs, err := store.ListReviewRunsBySession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("list review runs: %w", err)
	}
	events, err := store.ListSessionCIGateEvents(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("list ci gate events: %w", err)
	}
	var errs []error
	for _, pr := range open {
		if err := s.reconcileCIGatePR(ctx, store, pr, runs, events); err != nil {
			errs = append(errs, fmt.Errorf("pull request %s: %w", pr.URL, err))
		}
	}
	return errors.Join(errs...)
}

// SweepCIGates reconciles gated sessions every interval until ctx ends.
// Failed flips are logged and retried on the next tick.
func (s *ActionService) SweepCIGates(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepCIGatesOnce(ctx)
		}
	}
}

func (s *ActionService) sweepCIGatesOnce(ctx context.Context) {
	store, ok := s.store.(ciGateStore)
	if !ok {
		return
	}
	sessions, err := store.ListAllSessions(ctx)
	if err != nil {
		slog.Warn("ci gate sweep: list sessions failed", "error", err)
		return
	}
	for _, session := range sessions {
		if !session.CIGateActive {
			continue
		}
		if err := s.ReconcileCIGate(ctx, session.ID); err != nil {
			slog.Warn("ci gate sweep: reconcile failed", "session", session.ID, "error", err)
		}
	}
}

// reconcileCIGatePR flips one PR whose latest head event differs from its
// verdict. Reaching the desired state is recorded so it is not re-evaluated.
func (s *ActionService) reconcileCIGatePR(ctx context.Context, store ciGateStore, pr domain.PullRequest, runs []domain.ReviewRun, events []domain.SessionCIGateEvent) error {
	verdict, ok := currentHeadVerdict(pr, runs)
	if !ok {
		return nil
	}
	kind, wantDraft := "ci_gate_ready", false
	if verdict == domain.VerdictChangesRequested {
		kind, wantDraft = "ci_gate_draft", true
	}
	if latest, found := latestCIGateEvent(events, pr.URL, pr.HeadSHA); found && latest.Kind == kind {
		return nil
	}
	repo, ok := scmRepoForPR(pr)
	if !ok {
		return fmt.Errorf("%w: pull request repository is unknown", ErrPRPreconditions)
	}
	repo.Account = s.accountForPR(ctx, pr)
	ref := ports.SCMPRRef{Repo: repo, Number: pr.Number, URL: pr.URL}

	observations, err := s.reader.FetchPullRequests(ctx, []ports.SCMPRRef{ref})
	if err != nil {
		return fmt.Errorf("refresh pull request: %w", err)
	}
	if len(observations) != 1 || !observations[0].Fetched {
		return ErrPRNotFound
	}
	live := observations[0].PR
	if live.Merged || live.Closed || live.HeadSHA != pr.HeadSHA {
		// The head moved since the verdict; the new head is reviewed alone.
		return nil
	}
	reason := "already_in_state"
	if live.Draft != wantDraft {
		if err := s.draft.SetPullRequestDraft(ctx, ports.SCMDraftRequest{PR: ref, Draft: wantDraft}); err != nil {
			if errors.Is(err, ports.ErrSCMNotFound) {
				return fmt.Errorf("%w: %w", ErrPRNotFound, err)
			}
			return fmt.Errorf("set pull request draft state: %w", err)
		}
		reason = "verdict_" + string(verdict)
	}
	event := domain.SessionCIGateEvent{
		SessionID: pr.SessionID,
		PRURL:     pr.URL,
		HeadSHA:   pr.HeadSHA,
		Kind:      kind,
		Reason:    reason,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.RecordSessionCIGateEvent(ctx, event); err != nil {
		// The flip already happened; a retry would be a no-op on the provider.
		slog.Warn("ci gate: audit event not recorded", "session", pr.SessionID, "pr", pr.URL, "error", err)
	}
	return nil
}

// currentHeadVerdict returns the latest verdict recorded for the PR's head.
// Runs for other heads are ignored.
func currentHeadVerdict(pr domain.PullRequest, runs []domain.ReviewRun) (domain.ReviewVerdict, bool) {
	var (
		verdict domain.ReviewVerdict
		latest  time.Time
		found   bool
	)
	for _, run := range runs {
		if run.PRURL != pr.URL || run.TargetSHA != pr.HeadSHA {
			continue
		}
		if run.Status != domain.ReviewRunComplete && run.Status != domain.ReviewRunDelivered {
			continue
		}
		if run.ReviewedSHA != "" && run.ReviewedSHA != run.TargetSHA {
			continue
		}
		if run.Verdict != domain.VerdictApproved && run.Verdict != domain.VerdictChangesRequested {
			continue
		}
		if !found || run.CreatedAt.After(latest) {
			verdict, latest, found = run.Verdict, run.CreatedAt, true
		}
	}
	return verdict, found
}

// latestCIGateEvent returns the most recent event for one PR head.
func latestCIGateEvent(events []domain.SessionCIGateEvent, prURL, headSHA string) (domain.SessionCIGateEvent, bool) {
	var latest domain.SessionCIGateEvent
	found := false
	for _, ev := range events {
		if ev.PRURL == prURL && ev.HeadSHA == headSHA {
			latest, found = ev, true
		}
	}
	return latest, found
}
