package store

import (
	"context"
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
		HostLabel: link.HostLabel, DisplayName: link.DisplayName, CreatedAt: link.CreatedAt,
	}); err != nil {
		return fmt.Errorf("upsert remote worker link %s/%s: %w", link.HostID, link.RemoteSessionID, err)
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
		links[i] = domain.RemoteWorkerLink{
			HostID: row.HostID, HostLabel: row.HostLabel,
			RemoteSessionID: domain.SessionID(row.RemoteSessionID), RemoteProjectID: domain.ProjectID(row.RemoteProjectID),
			ProjectID: domain.ProjectID(row.ProjectID), DisplayName: row.DisplayName, CreatedAt: row.CreatedAt,
		}
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
