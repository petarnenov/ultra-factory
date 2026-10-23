package report

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ReadyStore is the durable state a ready-to-merge announcement reads.
type ReadyStore interface {
	recipientStore
	GetProject(context.Context, string) (domain.ProjectRecord, bool, error)
	ListPRsBySession(context.Context, domain.SessionID) ([]domain.PullRequest, error)
	ListPRComments(context.Context, string) ([]domain.PullRequestComment, error)
	ListReviewRunsBySession(context.Context, domain.SessionID) ([]domain.ReviewRun, error)
	ClaimPRMergeAnnouncement(context.Context, string, string, domain.SessionID, time.Time) (bool, error)
	ReleasePRMergeAnnouncement(context.Context, string) error
}

// ReadyAnnouncer files an AO report on a worker whose PR is ready to merge,
// so its coordinator or the project root wakes once per PR head.
type ReadyAnnouncer struct {
	store   ReadyStore
	reports *Service
	now     func() time.Time
}

// NewReadyAnnouncer constructs a ReadyAnnouncer that files through reports.
func NewReadyAnnouncer(store ReadyStore, reports *Service) *ReadyAnnouncer {
	return &ReadyAnnouncer{store: store, reports: reports, now: time.Now}
}

// AnnounceReadyToMerge reports each of worker's PRs whose stored facts are
// ready to merge. A PR head is announced at most once.
func (a *ReadyAnnouncer) AnnounceReadyToMerge(ctx context.Context, worker domain.SessionID) error {
	rec, found, err := a.store.GetSession(ctx, worker)
	if err != nil {
		return err
	}
	if !found || rec.Kind != domain.KindWorker || rec.IsTerminated || rec.ProjectID == "" {
		return nil
	}
	prs, err := a.store.ListPRsBySession(ctx, worker)
	if err != nil {
		return err
	}
	var errs []error
	for _, pr := range prs {
		errs = append(errs, a.announcePR(ctx, rec, pr))
	}
	return errors.Join(errs...)
}

func (a *ReadyAnnouncer) announcePR(ctx context.Context, worker domain.SessionRecord, pr domain.PullRequest) error {
	if pr.Number <= 0 || pr.HeadSHA == "" {
		return nil
	}
	ready, err := a.mergeReady(ctx, pr)
	if err != nil || !ready {
		return err
	}
	_, owner, _, _, err := resolveOwnerRecipient(ctx, a.store, worker.ProjectID, worker.OwnerSessionID)
	if err != nil {
		return err
	}
	coordinatorOwned := owner == ownerLive
	// A coordinator can hand a PR to the root only with an approved AO review.
	if worker.AutoReviewEnabled || coordinatorOwned {
		approved, err := a.headApproved(ctx, worker.ID, pr)
		if err != nil || !approved {
			return err
		}
	}
	note, err := a.readyNote(ctx, worker.ProjectID, pr, coordinatorOwned)
	if err != nil {
		return err
	}
	claimKey := fmt.Sprintf("ready:%s#%d@%s", pr.URL, pr.Number, pr.HeadSHA)
	won, err := a.store.ClaimPRMergeAnnouncement(ctx, claimKey, pr.URL, worker.ID, a.now().UTC())
	if err != nil || !won {
		return err
	}
	if err := a.reports.createLifecycleReport(ctx, worker, note); err != nil {
		return errors.Join(err, a.releaseClaim(ctx, claimKey))
	}
	return nil
}

// mergeReady applies the shared readiness rule to the stored PR facts.
func (a *ReadyAnnouncer) mergeReady(ctx context.Context, pr domain.PullRequest) (bool, error) {
	comments, err := a.store.ListPRComments(ctx, pr.URL)
	if err != nil {
		return false, err
	}
	unresolved := false
	for _, c := range comments {
		if domain.IsActionableReviewComment(c.Resolved, c.IsBot, c.File, c.Line) {
			unresolved = true
			break
		}
	}
	return domain.MergeReadinessOf(pr, unresolved).ReadyToMerge(), nil
}

// headApproved reports whether an AO review approved the PR's current head.
func (a *ReadyAnnouncer) headApproved(ctx context.Context, worker domain.SessionID, pr domain.PullRequest) (bool, error) {
	runs, err := a.store.ListReviewRunsBySession(ctx, worker)
	if err != nil {
		return false, err
	}
	for _, run := range runs {
		if run.PRURL == pr.URL && strings.EqualFold(run.TargetSHA, pr.HeadSHA) && isApprovedRun(run) {
			return true, nil
		}
	}
	return false, nil
}

func (a *ReadyAnnouncer) readyNote(ctx context.Context, projectID domain.ProjectID, pr domain.PullRequest, coordinatorOwned bool) (string, error) {
	head := fmt.Sprintf("AO: PR #%d is ready to merge at head %s (CI green, mergeable, no blocking review). %s", pr.Number, pr.HeadSHA, pr.URL)
	if coordinatorOwned {
		return fmt.Sprintf("%s\nHand it to the root with `ao coordinator report --ready-pr %d@%s`.", head, pr.Number, pr.HeadSHA), nil
	}
	project, found, err := a.store.GetProject(ctx, string(projectID))
	if err != nil {
		return "", err
	}
	if found && !project.Config.OrchestratorAutoMergeEnabled() {
		return head + "\nOrchestrator auto-merge is off for this project: tell the human it is ready instead of merging.", nil
	}
	return fmt.Sprintf("%s\nOrchestrator auto-merge is on for this project: re-check the head, then run `gh pr merge %d --squash --match-head-commit %s`.", head, pr.Number, pr.HeadSHA), nil
}

func (a *ReadyAnnouncer) releaseClaim(ctx context.Context, claimKey string) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	return a.store.ReleasePRMergeAnnouncement(cleanupCtx, claimKey)
}
