package report

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// PROutcomeClaims dedupes PR outcome reports across observer retries.
type PROutcomeClaims interface {
	ClaimPRMergeAnnouncement(ctx context.Context, key, prURL string, worker domain.SessionID, at time.Time) (bool, error)
	ReleasePRMergeAnnouncement(ctx context.Context, key string) error
}

// AnnounceMerged files a report on worker that its PR merged, once per merge.
// Delivery routes it to the owning coordinator, else the project root.
func (s *Service) AnnounceMerged(ctx context.Context, worker domain.SessionID, prURL string, number int, sha string) error {
	if number <= 0 {
		return nil
	}
	// Some providers report no merge commit (fast-forward or null mergeCommit).
	// The PR identity then dedupes the report, so it is never dropped.
	claimKey := sha
	commit := "merge commit " + sha
	if sha == "" {
		claimKey = fmt.Sprintf("pr:%s#%d", prURL, number)
		commit = "no merge commit reported"
	}
	note := fmt.Sprintf("AO: PR #%d merged (%s). %s", number, commit, prURL)
	return s.announcePROutcome(ctx, worker, prOutcome{claimKey: claimKey, prURL: prURL, note: note})
}

// AnnounceClosed files a report on worker that its PR closed without merge.
// The close time keys the claim, so a reopened PR that closes again reports.
func (s *Service) AnnounceClosed(ctx context.Context, worker domain.SessionID, prURL string, number int, closedAt time.Time) error {
	if number <= 0 {
		return nil
	}
	claimKey := fmt.Sprintf("closed:%s#%d", prURL, number)
	if !closedAt.IsZero() {
		claimKey += "@" + closedAt.UTC().Format(time.RFC3339)
	}
	return s.announcePROutcome(ctx, worker, prOutcome{
		claimKey: claimKey,
		prURL:    prURL,
		note:     fmt.Sprintf("AO: PR #%d closed without merge. %s", number, prURL),
	})
}

type prOutcome struct {
	claimKey string
	prURL    string
	note     string
}

func (s *Service) announcePROutcome(ctx context.Context, worker domain.SessionID, outcome prOutcome) error {
	if s.claims == nil {
		return errors.New("report: PR outcome claims are required")
	}
	session, ok, err := s.store.GetSession(ctx, worker)
	if err != nil {
		return err
	}
	if !ok || session.Kind != domain.KindWorker || session.ProjectID == "" {
		return nil
	}
	won, err := s.claims.ClaimPRMergeAnnouncement(ctx, outcome.claimKey, outcome.prURL, worker, s.now().UTC())
	if err != nil || !won {
		return err
	}
	if err := s.createLifecycleReport(ctx, session, outcome.note); err != nil {
		return errors.Join(err, s.releasePROutcomeClaim(ctx, outcome.claimKey))
	}
	return nil
}
