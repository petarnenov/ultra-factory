package store

import (
	"context"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// ClaimPRMergeAnnouncement records that a merge commit is being announced. It
// reports false when another path already claimed the same merge commit.
func (s *Store) ClaimPRMergeAnnouncement(ctx context.Context, mergeCommitSHA, prURL string, sessionID domain.SessionID, at time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.ClaimPRMergeAnnouncement(ctx, gen.ClaimPRMergeAnnouncementParams{
		MergeCommitSha: mergeCommitSHA, PRURL: prURL, SessionID: string(sessionID),
		AnnouncedAt: at.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return false, fmt.Errorf("claim pr merge announcement: %w", err)
	}
	return rows == 1, nil
}

// ReleasePRMergeAnnouncement drops a claim whose send failed, so a later pass
// can announce the merge again.
func (s *Store) ReleasePRMergeAnnouncement(ctx context.Context, mergeCommitSHA string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.ReleasePRMergeAnnouncement(ctx, mergeCommitSHA); err != nil {
		return fmt.Errorf("release pr merge announcement: %w", err)
	}
	return nil
}
