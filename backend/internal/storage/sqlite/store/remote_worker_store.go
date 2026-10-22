package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// UpsertRemoteWorkerLink records or refreshes a remote worker's link. A
// refreshed link keeps its creation time and its delivered-report history.
func (s *Store) UpsertRemoteWorkerLink(ctx context.Context, link domain.RemoteWorkerLink) error {
	if err := link.Validate(); err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.UpsertRemoteWorkerLink(ctx, gen.UpsertRemoteWorkerLinkParams{
		HostID: link.HostID, RemoteSessionID: string(link.RemoteSessionID),
		RemoteProjectID: string(link.RemoteProjectID), ProjectID: string(link.ProjectID),
		HostLabel: link.HostLabel, DisplayName: link.DisplayName,
		OwnerSessionID: link.OwnerSessionID, CreatedAt: link.CreatedAt,
	}); err != nil {
		return fmt.Errorf("upsert remote worker link %s/%s: %w", link.HostID, link.RemoteSessionID, err)
	}
	return nil
}

// GetRemoteWorkerLink returns one link by its durable identity.
func (s *Store) GetRemoteWorkerLink(ctx context.Context, hostID string, remoteSessionID domain.SessionID) (domain.RemoteWorkerLink, bool, error) {
	row, err := s.qr.GetRemoteWorkerLink(ctx, gen.GetRemoteWorkerLinkParams{
		HostID: hostID, RemoteSessionID: string(remoteSessionID),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return domain.RemoteWorkerLink{}, false, nil
	}
	if err != nil {
		return domain.RemoteWorkerLink{}, false, fmt.Errorf("get remote worker link %s/%s: %w", hostID, remoteSessionID, err)
	}
	return remoteLinkFromRow(row), true, nil
}

// InsertDiscoveredRemoteWorkerLink stores a discovered link if none exists;
// an existing link keeps its owner and source.
func (s *Store) InsertDiscoveredRemoteWorkerLink(ctx context.Context, link domain.RemoteWorkerLink) (bool, error) {
	if err := link.Validate(); err != nil {
		return false, err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	affected, err := s.qw.InsertDiscoveredRemoteWorkerLink(ctx, gen.InsertDiscoveredRemoteWorkerLinkParams{
		HostID: link.HostID, RemoteSessionID: string(link.RemoteSessionID),
		RemoteProjectID: string(link.RemoteProjectID), ProjectID: string(link.ProjectID),
		HostLabel: link.HostLabel, DisplayName: link.DisplayName, CreatedAt: link.CreatedAt,
	})
	if err != nil {
		return false, fmt.Errorf("insert discovered remote worker link %s/%s: %w", link.HostID, link.RemoteSessionID, err)
	}
	return affected > 0, nil
}

func remoteLinkFromRow(row gen.RemoteWorkerLink) domain.RemoteWorkerLink {
	link := domain.RemoteWorkerLink{
		HostID: row.HostID, HostLabel: row.HostLabel,
		RemoteSessionID: domain.SessionID(row.RemoteSessionID), RemoteProjectID: domain.ProjectID(row.RemoteProjectID),
		ProjectID: domain.ProjectID(row.ProjectID), DisplayName: row.DisplayName,
		OwnerSessionID: row.OwnerSessionID, Source: domain.LinkSource(row.Source), CreatedAt: row.CreatedAt,
		LastState: row.LastState,
	}
	if row.LastSeenAt.Valid {
		seen := row.LastSeenAt.Time
		link.LastSeenAt = &seen
	}
	return link
}

// UpdateRemoteWorkerLinkLiveness records the outcome of a read of a link's
// worker: when the host last answered, and whether the worker was live.
func (s *Store) UpdateRemoteWorkerLinkLiveness(ctx context.Context, hostID string, remoteSessionID domain.SessionID, at time.Time, state string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.UpdateRemoteWorkerLinkLiveness(ctx, gen.UpdateRemoteWorkerLinkLivenessParams{
		LastSeenAt: sql.NullTime{Time: at, Valid: true}, LastState: state,
		HostID: hostID, RemoteSessionID: string(remoteSessionID),
	}); err != nil {
		return fmt.Errorf("update remote worker link liveness %s/%s: %w", hostID, remoteSessionID, err)
	}
	return nil
}

// RemoveRemoteWorkerLink drops a link by hand and remembers the removal in
// the same transaction, so discovery does not link a terminated worker again.
func (s *Store) RemoveRemoteWorkerLink(ctx context.Context, hostID string, remoteSessionID domain.SessionID, at time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	err := s.inTx(ctx, "remove remote worker link", func(q *gen.Queries) error {
		if err := q.DeleteRemoteWorkerLink(ctx, gen.DeleteRemoteWorkerLinkParams{
			HostID: hostID, RemoteSessionID: string(remoteSessionID),
		}); err != nil {
			return err
		}
		return q.InsertRemoteWorkerLinkRemoval(ctx, gen.InsertRemoteWorkerLinkRemovalParams{
			HostID: hostID, RemoteSessionID: string(remoteSessionID), RemovedAt: at,
		})
	})
	if err != nil {
		return fmt.Errorf("remove remote worker link %s/%s: %w", hostID, remoteSessionID, err)
	}
	return nil
}

// ListRemoteWorkerLinkRemovals returns every removal a link was dropped by.
func (s *Store) ListRemoteWorkerLinkRemovals(ctx context.Context) ([]domain.RemoteWorkerLinkRemoval, error) {
	rows, err := s.qr.ListRemoteWorkerLinkRemovals(ctx)
	if err != nil {
		return nil, fmt.Errorf("list remote worker link removals: %w", err)
	}
	removals := make([]domain.RemoteWorkerLinkRemoval, len(rows))
	for i, row := range rows {
		removals[i] = domain.RemoteWorkerLinkRemoval{
			HostID: row.HostID, RemoteSessionID: domain.SessionID(row.RemoteSessionID), RemovedAt: row.RemovedAt,
		}
	}
	return removals, nil
}

// DeleteRemoteWorkerLinkRemoval forgets a removal, e.g. once a live worker
// is linked again.
func (s *Store) DeleteRemoteWorkerLinkRemoval(ctx context.Context, hostID string, remoteSessionID domain.SessionID) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.DeleteRemoteWorkerLinkRemoval(ctx, gen.DeleteRemoteWorkerLinkRemovalParams{
		HostID: hostID, RemoteSessionID: string(remoteSessionID),
	}); err != nil {
		return fmt.Errorf("delete remote worker link removal %s/%s: %w", hostID, remoteSessionID, err)
	}
	return nil
}

// ListRemoteWorkerLinks returns every link, grouped by host.
func (s *Store) ListRemoteWorkerLinks(ctx context.Context) ([]domain.RemoteWorkerLink, error) {
	rows, err := s.qr.ListRemoteWorkerLinks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list remote worker links: %w", err)
	}
	links := make([]domain.RemoteWorkerLink, len(rows))
	for i, row := range rows {
		links[i] = remoteLinkFromRow(row)
	}
	return links, nil
}

// DeleteRemoteWorkerLink removes a link and, by cascade, its delivery history.
func (s *Store) DeleteRemoteWorkerLink(ctx context.Context, hostID string, remoteSessionID domain.SessionID) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.DeleteRemoteWorkerLink(ctx, gen.DeleteRemoteWorkerLinkParams{
		HostID: hostID, RemoteSessionID: string(remoteSessionID),
	}); err != nil {
		return fmt.Errorf("delete remote worker link %s/%s: %w", hostID, remoteSessionID, err)
	}
	return nil
}

// ListDeliveredRemoteReports returns the remote report IDs of a link that were
// already delivered to the local orchestrator.
func (s *Store) ListDeliveredRemoteReports(ctx context.Context, hostID string, remoteSessionID domain.SessionID) (map[string]bool, error) {
	ids, err := s.qr.ListRemoteWorkerReportDeliveries(ctx, gen.ListRemoteWorkerReportDeliveriesParams{
		HostID: hostID, RemoteSessionID: string(remoteSessionID),
	})
	if err != nil {
		return nil, fmt.Errorf("list delivered remote reports %s/%s: %w", hostID, remoteSessionID, err)
	}
	delivered := make(map[string]bool, len(ids))
	for _, id := range ids {
		delivered[id] = true
	}
	return delivered, nil
}

// RecordDeliveredRemoteReports marks remote report IDs of a link as delivered.
// Recording an ID twice is a no-op.
func (s *Store) RecordDeliveredRemoteReports(ctx context.Context, hostID string, remoteSessionID domain.SessionID, reportIDs []string, at time.Time) error {
	if len(reportIDs) == 0 {
		return nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	err := s.inTx(ctx, "record delivered remote reports", func(q *gen.Queries) error {
		for _, id := range reportIDs {
			if err := q.InsertRemoteWorkerReportDelivery(ctx, gen.InsertRemoteWorkerReportDeliveryParams{
				HostID: hostID, RemoteSessionID: string(remoteSessionID), ReportID: id, DeliveredAt: at,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("record delivered remote reports %s/%s: %w", hostID, remoteSessionID, err)
	}
	return nil
}

// RecordRemoteReportHistory retains delivered report content under the full
// remote identity. Replays are idempotent even when hosts reuse report IDs.
func (s *Store) RecordRemoteReportHistory(ctx context.Context, hostID string, remoteSessionID domain.SessionID, reports []domain.ReportRecord) error {
	if len(reports) == 0 {
		return nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin remote report history transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, report := range reports {
		result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO remote_worker_reports
			(host_id, remote_session_id, report_id, project_id, source_project_id, state, note, message, agent, session_name, host_label, created_at, repeat_count)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			hostID, string(remoteSessionID), report.ID, string(report.ProjectID), report.SourceProjectID, string(report.State), report.Note, report.Message,
			report.Agent, report.SessionDisplayName, report.HostLabel, report.CreatedAt, report.RepeatCount)
		if err != nil {
			return fmt.Errorf("record remote report history %s/%s/%s: %w", hostID, remoteSessionID, report.ID, err)
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("check remote report history %s/%s/%s: %w", hostID, remoteSessionID, report.ID, err)
		}
		if inserted == 0 {
			continue
		}
		for position, output := range report.Outputs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO remote_worker_report_outputs
				(host_id, remote_session_id, report_id, position, kind, reference, label) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				hostID, string(remoteSessionID), report.ID, position, string(output.Kind), output.Reference, output.Label); err != nil {
				return fmt.Errorf("record remote report output %s/%s/%s: %w", hostID, remoteSessionID, report.ID, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit remote report history: %w", err)
	}
	return nil
}
