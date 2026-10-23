package report

import (
	"context"
	"time"
)

// releasePROutcomeClaim drops a claim whose report was not stored, so the
// next observation announces that PR outcome again.
func (s *Service) releasePROutcomeClaim(ctx context.Context, key string) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	return s.claims.ReleasePRMergeAnnouncement(cleanupCtx, key)
}
