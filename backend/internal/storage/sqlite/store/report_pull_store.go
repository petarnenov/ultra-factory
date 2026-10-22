package store

import (
	"context"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// TouchReportPuller records pullerID's pull of sessionID at at. It returns the
// monotonic expired total and the puller's acked IDs, changing no expiry state.
func (s *Store) TouchReportPuller(ctx context.Context, sessionID domain.SessionID, pullerID string, at time.Time) (int64, map[string]bool, error) {
	if sessionID == "" || pullerID == "" || at.IsZero() {
		return 0, nil, domain.ErrInvalidReport
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var (
		total int64
		acked []string
	)
	err := s.inTx(ctx, "touch report puller", func(q *gen.Queries) error {
		if err := q.TouchReportPuller(ctx, gen.TouchReportPullerParams{
			SessionID: string(sessionID), PullerID: pullerID, PulledAt: at,
		}); err != nil {
			return err
		}
		var err error
		total, err = q.GetReportPullerExpiredTotal(ctx, gen.GetReportPullerExpiredTotalParams{
			SessionID: string(sessionID), PullerID: pullerID,
		})
		if err != nil {
			return err
		}
		acked, err = q.ListReportPullerAckedIDs(ctx, gen.ListReportPullerAckedIDsParams{
			SessionID: string(sessionID), PullerID: pullerID,
		})
		return err
	})
	if err != nil {
		return 0, nil, fmt.Errorf("touch report puller for %s: %w", sessionID, err)
	}
	ackedSet := make(map[string]bool, len(acked))
	for _, id := range acked {
		ackedSet[id] = true
	}
	return total, ackedSet, nil
}

// CompactExpiredReports expires pending reports older than cutoff or beyond the
// newest keep. Unacked pullers are counted first. It returns the expired count.
func (s *Store) CompactExpiredReports(ctx context.Context, sessionID domain.SessionID, cutoff time.Time, keep int, at time.Time) (int64, error) {
	if sessionID == "" || cutoff.IsZero() || keep < 1 || at.IsZero() {
		return 0, domain.ErrInvalidReport
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var expired int64
	err := s.inTx(ctx, "compact expired reports", func(q *gen.Queries) error {
		if err := q.AddExpiredToReportPullers(ctx, gen.AddExpiredToReportPullersParams{
			SessionID: string(sessionID), Cutoff: cutoff, Keep: int64(keep),
		}); err != nil {
			return err
		}
		n, err := q.ExpirePendingSessionReports(ctx, gen.ExpirePendingSessionReportsParams{
			SessionID: string(sessionID), Cutoff: cutoff, Keep: int64(keep), ExpiredAt: nullTime(at),
		})
		expired = n
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("compact expired reports for %s: %w", sessionID, err)
	}
	return expired, nil
}

// AcknowledgePendingSessionReportsFor records pullerID's ack of reports in
// sessionID. Other pullers and the worker's own queue are unaffected.
func (s *Store) AcknowledgePendingSessionReportsFor(ctx context.Context, sessionID domain.SessionID, ids []string, pullerID string, at time.Time) ([]string, error) {
	if sessionID == "" || pullerID == "" || at.IsZero() {
		return nil, domain.ErrInvalidReport
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var acknowledged []string
	err := s.inTx(ctx, "acknowledge pulled reports for puller", func(q *gen.Queries) error {
		for _, id := range ids {
			n, err := q.AcknowledgeReportForPuller(ctx, gen.AcknowledgeReportForPullerParams{
				PullerID: pullerID, AcknowledgedAt: at, ID: id, SessionID: string(sessionID),
			})
			if err != nil {
				return err
			}
			if n > 0 {
				acknowledged = append(acknowledged, id)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("acknowledge pulled reports for %s: %w", sessionID, err)
	}
	return acknowledged, nil
}
