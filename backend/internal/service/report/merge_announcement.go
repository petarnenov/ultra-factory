package report

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// AnnounceMerged tells the coordinator that owns worker its PR merged. The
// observer calls it once per merge; root-owned workers need no notice.
func (c *Coordinator) AnnounceMerged(ctx context.Context, worker domain.SessionID, prURL string, number int, sha string) error {
	if number <= 0 {
		return nil
	}
	// Some providers report no merge commit (fast-forward or null mergeCommit).
	// The PR identity then dedupes the notice, so it is never dropped.
	claimKey := sha
	if claimKey == "" {
		claimKey = fmt.Sprintf("pr:%s#%d", prURL, number)
	}
	rec, found, err := c.store.GetSession(ctx, worker)
	if err != nil {
		return err
	}
	if !found || rec.Kind != domain.KindWorker || rec.OwnerSessionID == nil {
		return nil
	}
	owner := *rec.OwnerSessionID
	recipient, found, err := c.store.GetSession(ctx, owner)
	if err != nil {
		return err
	}
	if !found || recipient.IsTerminated || recipient.Kind != domain.KindCoordinator ||
		recipient.ProjectID != rec.ProjectID || recipient.Activity.State == domain.ActivityExited {
		return nil
	}
	won, err := c.store.ClaimPRMergeAnnouncement(ctx, claimKey, prURL, worker, c.now().UTC())
	if err != nil || !won {
		return err
	}
	commit := "no merge commit reported"
	if sha != "" {
		commit = "merge commit " + sha
	}
	message := fmt.Sprintf("PR #%d for worker %s merged (%s). Your next task may depend on it.",
		number, worker, commit)
	key := fmt.Sprintf("pr-merged:%x", sha256.Sum256([]byte(claimKey)))
	err = c.delivery.Submit(ctx, owner, message, key)
	if errors.Is(err, domain.ErrSemanticAcceptanceTimeout) {
		return nil
	}
	if err != nil {
		return errors.Join(err, c.releaseMergeClaims(ctx, []string{claimKey}))
	}
	return nil
}
