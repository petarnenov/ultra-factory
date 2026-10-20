package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// CreateCoordinatorSummary persists one pending summary and its worker links.
// A duplicate (coordinator, submission key) returns the original, unwritten.
func (s *Store) CreateCoordinatorSummary(ctx context.Context, rec domain.CoordinatorSummary) (domain.CoordinatorSummary, bool, error) {
	if rec.ID == "" || rec.CoordinatorSessionID == "" || rec.ProjectID == "" || rec.CreatedAt.IsZero() || rec.AvailableAt.IsZero() {
		return domain.CoordinatorSummary{}, false, domain.ErrInvalidCoordinatorSummary
	}
	if err := domain.ValidateCoordinatorSummary(rec); err != nil {
		return domain.CoordinatorSummary{}, false, err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var out gen.CoordinatorSummary
	created := false
	err := s.inTx(ctx, "create coordinator summary", func(q *gen.Queries) error {
		row, err := q.InsertCoordinatorSummary(ctx, gen.InsertCoordinatorSummaryParams{
			ID: rec.ID, CoordinatorSessionID: string(rec.CoordinatorSessionID), ProjectID: string(rec.ProjectID),
			SubmissionKey: rec.SubmissionKey, Scope: rec.Scope, State: string(rec.State), Result: rec.Result,
			EscalationReason: string(rec.EscalationReason), EscalationNote: rec.EscalationNote,
			CreatedAt: rec.CreatedAt, AvailableAt: rec.AvailableAt,
		})
		if errors.Is(err, sql.ErrNoRows) {
			out, err = q.GetCoordinatorSummaryByKey(ctx, gen.GetCoordinatorSummaryByKeyParams{
				CoordinatorSessionID: string(rec.CoordinatorSessionID), SubmissionKey: rec.SubmissionKey,
			})
			return err
		}
		if err != nil {
			return err
		}
		for i, worker := range rec.WorkerSessionIDs {
			if err := q.InsertCoordinatorSummaryWorker(ctx, gen.InsertCoordinatorSummaryWorkerParams{
				SummaryID: row.ID, WorkerSessionID: string(worker), Position: int64(i),
			}); err != nil {
				return err
			}
		}
		out, created = row, true
		return nil
	})
	if err != nil {
		return domain.CoordinatorSummary{}, false, fmt.Errorf("create coordinator summary: %w", err)
	}
	summary, err := s.summaryWithWorkers(ctx, out)
	return summary, created, err
}

// GetCoordinatorSummaryByKey looks up a prior submission for replay.
func (s *Store) GetCoordinatorSummaryByKey(ctx context.Context, coordinator domain.SessionID, key string) (domain.CoordinatorSummary, bool, error) {
	row, err := s.qr.GetCoordinatorSummaryByKey(ctx, gen.GetCoordinatorSummaryByKeyParams{
		CoordinatorSessionID: string(coordinator), SubmissionKey: key,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CoordinatorSummary{}, false, nil
	}
	if err != nil {
		return domain.CoordinatorSummary{}, false, fmt.Errorf("get coordinator summary by key: %w", err)
	}
	summary, err := s.summaryWithWorkers(ctx, row)
	return summary, err == nil, err
}

// ListCoordinatorSummaries returns one coordinator's summaries newest first.
// History is retained; acknowledged rows are never removed.
func (s *Store) ListCoordinatorSummaries(ctx context.Context, coordinator domain.SessionID, limit int64) ([]domain.CoordinatorSummary, error) {
	rows, err := s.qr.ListCoordinatorSummariesBySession(ctx, gen.ListCoordinatorSummariesBySessionParams{
		CoordinatorSessionID: string(coordinator), Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list coordinator summaries: %w", err)
	}
	return s.summariesWithWorkers(ctx, rows)
}

// GetLatestAcceptedCoordinatorSummary returns the newest summary the root has
// accepted for coordinator.
func (s *Store) GetLatestAcceptedCoordinatorSummary(ctx context.Context, coordinator domain.SessionID) (domain.CoordinatorSummary, bool, error) {
	row, err := s.qr.GetLatestAcknowledgedCoordinatorSummary(ctx, string(coordinator))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CoordinatorSummary{}, false, nil
	}
	if err != nil {
		return domain.CoordinatorSummary{}, false, fmt.Errorf("get latest accepted coordinator summary: %w", err)
	}
	summary, err := s.summaryWithWorkers(ctx, row)
	return summary, err == nil, err
}

// ListPendingCoordinatorSummarySchedule returns the earliest pending summaries
// regardless of availability so the scheduler restores deadlines after restart.
func (s *Store) ListPendingCoordinatorSummarySchedule(ctx context.Context, limit int64) ([]domain.CoordinatorSummary, error) {
	rows, err := s.qr.ListPendingCoordinatorSummarySchedule(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list coordinator summary schedule: %w", err)
	}
	return s.summariesWithWorkers(ctx, rows)
}

// ClaimPendingCoordinatorSummaryBatch claims one coordinator's due summaries
// for the root. A retry reuses the first claim's batch identity.
func (s *Store) ClaimPendingCoordinatorSummaryBatch(ctx context.Context, projectID domain.ProjectID, coordinator, root domain.SessionID, token string, at time.Time) ([]domain.CoordinatorSummary, error) {
	if projectID == "" || coordinator == "" || root == "" || token == "" || at.IsZero() {
		return nil, domain.ErrInvalidCoordinatorSummary
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var rows []gen.CoordinatorSummary
	err := s.inTx(ctx, "claim coordinator summary batch", func(q *gen.Queries) error {
		all, err := q.ListPendingCoordinatorSummariesByGroup(ctx, gen.ListPendingCoordinatorSummariesByGroupParams{
			ProjectID: string(projectID), CoordinatorSessionID: string(coordinator),
		})
		if err != nil {
			return err
		}
		pending := all[:0:0]
		for _, row := range all {
			if !row.AvailableAt.After(at) {
				pending = append(pending, row)
			}
		}
		if len(pending) == 0 {
			return nil
		}
		batchID := pending[0].DeliveryBatchID
		if batchID == "" {
			ids := make([]string, 0, len(pending))
			for _, row := range pending {
				if row.DeliveryBatchID == "" {
					ids = append(ids, row.ID)
				}
			}
			batchID = domain.CoordinatorSummaryBatchID(ids)
			if err := q.AssignPendingCoordinatorSummaryBatch(ctx, gen.AssignPendingCoordinatorSummaryBatchParams{
				DeliveryBatchID: batchID, ProjectID: string(projectID), CoordinatorSessionID: string(coordinator), Ids: ids,
			}); err != nil {
				return err
			}
		}
		rows, err = q.ClaimPendingCoordinatorSummaries(ctx, gen.ClaimPendingCoordinatorSummariesParams{
			ProjectID: string(projectID), CoordinatorSessionID: string(coordinator), DeliveryBatchID: batchID,
			RootSessionID: string(root), ClaimToken: token, ClaimedAt: nullTime(at),
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("claim coordinator summary batch: %w", err)
	}
	return s.summariesWithWorkers(ctx, orderedSummaryRows(rows))
}

// AcknowledgeCoordinatorSummaryBatch completes only the batch holding token.
func (s *Store) AcknowledgeCoordinatorSummaryBatch(ctx context.Context, projectID domain.ProjectID, token string, at time.Time) (int, error) {
	if projectID == "" || token == "" || at.IsZero() {
		return 0, domain.ErrInvalidCoordinatorSummary
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.AcknowledgeCoordinatorSummaryBatch(ctx, gen.AcknowledgeCoordinatorSummaryBatchParams{
		ProjectID: string(projectID), ClaimToken: token, AcknowledgedAt: nullTime(at),
	})
	if err != nil {
		return 0, fmt.Errorf("acknowledge coordinator summary batch: %w", err)
	}
	return len(rows), nil
}

// ReleaseCoordinatorSummaryBatch returns the batch to pending without moving
// any summary's deadline later.
func (s *Store) ReleaseCoordinatorSummaryBatch(ctx context.Context, projectID domain.ProjectID, token string, at time.Time, lastError string) (int, error) {
	if projectID == "" || token == "" || at.IsZero() {
		return 0, domain.ErrInvalidCoordinatorSummary
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.ReleaseCoordinatorSummaryBatch(ctx, gen.ReleaseCoordinatorSummaryBatchParams{
		ProjectID: string(projectID), ClaimToken: token, AvailableAt: at, LastError: lastError,
	})
	if err != nil {
		return 0, fmt.Errorf("release coordinator summary batch: %w", err)
	}
	return len(rows), nil
}

// DeferCoordinatorSummaryBatch returns the batch to pending and moves its
// availability later. Nothing is dropped.
func (s *Store) DeferCoordinatorSummaryBatch(ctx context.Context, projectID domain.ProjectID, token string, at time.Time, lastError string) (int, error) {
	if projectID == "" || token == "" || at.IsZero() {
		return 0, domain.ErrInvalidCoordinatorSummary
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.DeferCoordinatorSummaryBatch(ctx, gen.DeferCoordinatorSummaryBatchParams{
		ProjectID: string(projectID), ClaimToken: token, AvailableAt: at, LastError: lastError,
	})
	if err != nil {
		return 0, fmt.Errorf("defer coordinator summary batch: %w", err)
	}
	return len(rows), nil
}

// RequeueClaimedCoordinatorSummaries recovers claims owned by a prior daemon.
func (s *Store) RequeueClaimedCoordinatorSummaries(ctx context.Context) (int64, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	count, err := s.qw.RequeueClaimedCoordinatorSummaries(ctx)
	if err != nil {
		return 0, fmt.Errorf("requeue claimed coordinator summaries: %w", err)
	}
	return count, nil
}

func (s *Store) summaryWithWorkers(ctx context.Context, row gen.CoordinatorSummary) (domain.CoordinatorSummary, error) {
	workers, err := s.qr.ListCoordinatorSummaryWorkers(ctx, row.ID)
	if err != nil {
		return domain.CoordinatorSummary{}, fmt.Errorf("list coordinator summary workers: %w", err)
	}
	remotes, err := s.qr.ListCoordinatorSummaryRemoteWorkers(ctx, row.ID)
	if err != nil {
		return domain.CoordinatorSummary{}, fmt.Errorf("list coordinator summary remote workers: %w", err)
	}
	rec := coordinatorSummaryFromGen(row)
	for _, r := range remotes {
		rec.RemoteWorkers = append(rec.RemoteWorkers, domain.SummaryRemoteWorker{HostID: r.HostID, RemoteSessionID: r.RemoteSessionID})
	}
	rec.WorkerSessionIDs = make([]domain.SessionID, len(workers))
	for i, worker := range workers {
		rec.WorkerSessionIDs[i] = domain.SessionID(worker)
	}
	return rec, nil
}

func (s *Store) summariesWithWorkers(ctx context.Context, rows []gen.CoordinatorSummary) ([]domain.CoordinatorSummary, error) {
	out := make([]domain.CoordinatorSummary, len(rows))
	for i, row := range rows {
		rec, err := s.summaryWithWorkers(ctx, row)
		if err != nil {
			return nil, err
		}
		out[i] = rec
	}
	return out, nil
}

func orderedSummaryRows(rows []gen.CoordinatorSummary) []gen.CoordinatorSummary {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].CreatedAt.Before(rows[j].CreatedAt)
	})
	return rows
}

func coordinatorSummaryFromGen(r gen.CoordinatorSummary) domain.CoordinatorSummary {
	return domain.CoordinatorSummary{
		ID: r.ID, CoordinatorSessionID: domain.SessionID(r.CoordinatorSessionID), ProjectID: domain.ProjectID(r.ProjectID),
		SubmissionKey: r.SubmissionKey, Scope: r.Scope, State: domain.CoordinatorSummaryState(r.State), Result: r.Result,
		EscalationReason: domain.EscalationReason(r.EscalationReason), EscalationNote: r.EscalationNote,
		CreatedAt: r.CreatedAt, AvailableAt: r.AvailableAt, DeliveryBatchID: r.DeliveryBatchID,
		DeliveryOwnerID: r.DeliveryOwnerID, DeliveryState: domain.ReportDeliveryState(r.DeliveryState),
		ClaimToken: r.ClaimToken, ClaimedAt: timeFromNull(r.ClaimedAt), DeliveryAttempts: r.DeliveryAttempts,
		AcknowledgedAt: timeFromNull(r.AcknowledgedAt), LastError: r.LastError,
	}
}
