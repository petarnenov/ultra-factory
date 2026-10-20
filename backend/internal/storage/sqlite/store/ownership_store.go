package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// SetSessionOwner changes a worker's owner and appends audit in one tx.
// Same owner is a no-op; skipClaimGuard is only for automatic repair.
func (s *Store) SetSessionOwner(
	ctx context.Context,
	id domain.SessionID,
	owner *domain.SessionID,
	reason domain.OwnershipEventReason,
	at time.Time,
	skipClaimGuard bool,
) (domain.SessionRecord, bool, error) {
	return s.setSessionOwner(ctx, id, owner, nil, reason, at, skipClaimGuard)
}

// ClearSessionOwnerIf clears a worker's owner only while the stored owner
// still equals expected; otherwise it changes nothing and writes no audit.
func (s *Store) ClearSessionOwnerIf(
	ctx context.Context,
	id domain.SessionID,
	expected domain.SessionID,
	reason domain.OwnershipEventReason,
	at time.Time,
) (domain.SessionRecord, bool, error) {
	return s.setSessionOwner(ctx, id, nil, &expected, reason, at, true)
}

func (s *Store) setSessionOwner(
	ctx context.Context,
	id domain.SessionID,
	owner *domain.SessionID,
	expected *domain.SessionID,
	reason domain.OwnershipEventReason,
	at time.Time,
	skipClaimGuard bool,
) (domain.SessionRecord, bool, error) {
	if !reason.Valid() {
		return domain.SessionRecord{}, false, fmt.Errorf("invalid ownership reason %q", reason)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var record domain.SessionRecord
	changed := false
	err := s.inTx(ctx, "set session owner", func(q *gen.Queries) error {
		row, err := q.GetSession(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return ports.ErrSessionNotFound
		}
		if err != nil {
			return err
		}
		current := rowToRecord(row)
		if expected != nil && !sameOwner(current.OwnerSessionID, expected) {
			record = current
			return nil
		}
		if sameOwner(current.OwnerSessionID, owner) {
			record = current
			return nil
		}
		eventReason := reason
		if reason == domain.OwnershipExplicitAssignment || reason == domain.OwnershipExplicitReassignment {
			if current.OwnerSessionID == nil {
				eventReason = domain.OwnershipExplicitAssignment
			} else {
				eventReason = domain.OwnershipExplicitReassignment
			}
		}
		if err := q.InsertSessionOwnerEvent(ctx, gen.InsertSessionOwnerEventParams{
			SessionID:              id,
			PreviousOwnerSessionID: current.OwnerSessionID,
			NewOwnerSessionID:      owner,
			Reason:                 eventReason,
			CreatedAt:              at,
		}); err != nil {
			return err
		}
		var rows int64
		if !skipClaimGuard {
			rows, err = q.SetSessionOwnerUnlessReportClaimed(ctx, gen.SetSessionOwnerUnlessReportClaimedParams{
				OwnerSessionID: owner,
				UpdatedAt:      at,
				ID:             id,
			})
		} else {
			rows, err = q.SetSessionOwner(ctx, gen.SetSessionOwnerParams{
				OwnerSessionID: owner,
				UpdatedAt:      at,
				ID:             id,
			})
		}
		if err != nil {
			return err
		}
		if rows != 1 {
			if !skipClaimGuard && current.Kind == domain.KindWorker {
				return ports.ErrOwnerReassignmentInFlight
			}
			return fmt.Errorf("session %s is not a worker", id)
		}
		updated, err := q.GetSession(ctx, id)
		if err != nil {
			return err
		}
		record = rowToRecord(updated)
		changed = true
		return nil
	})
	if err != nil {
		return domain.SessionRecord{}, false, fmt.Errorf("set session owner %s: %w", id, err)
	}
	return record, changed, nil
}

// ListSessionOwnerEvents returns one worker's ownership history in write order.
func (s *Store) ListSessionOwnerEvents(ctx context.Context, id domain.SessionID) ([]domain.SessionOwnerEvent, error) {
	rows, err := s.qr.ListSessionOwnerEvents(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list session owner events %s: %w", id, err)
	}
	events := make([]domain.SessionOwnerEvent, len(rows))
	for i, row := range rows {
		events[i] = domain.SessionOwnerEvent{
			ID:                     row.ID,
			SessionID:              row.SessionID,
			PreviousOwnerSessionID: row.PreviousOwnerSessionID,
			NewOwnerSessionID:      row.NewOwnerSessionID,
			Reason:                 row.Reason,
			CreatedAt:              row.CreatedAt,
		}
	}
	return events, nil
}

// SetRemoteWorkerOwner changes a link's owner and appends the audit row in one
// transaction. It never moves the link between local projects.
func (s *Store) SetRemoteWorkerOwner(
	ctx context.Context,
	hostID string,
	remoteSessionID domain.SessionID,
	owner *domain.SessionID,
	reason domain.OwnershipEventReason,
	at time.Time,
) (domain.RemoteWorkerLink, bool, error) {
	return s.setRemoteWorkerOwner(ctx, hostID, remoteSessionID, owner, nil, reason, at)
}

// ClearRemoteWorkerOwnerIf clears a link's owner only while the stored owner
// still equals expected; otherwise it changes nothing and writes no audit.
func (s *Store) ClearRemoteWorkerOwnerIf(
	ctx context.Context,
	hostID string,
	remoteSessionID domain.SessionID,
	expected domain.SessionID,
	reason domain.OwnershipEventReason,
	at time.Time,
) (domain.RemoteWorkerLink, bool, error) {
	return s.setRemoteWorkerOwner(ctx, hostID, remoteSessionID, nil, &expected, reason, at)
}

func (s *Store) setRemoteWorkerOwner(
	ctx context.Context,
	hostID string,
	remoteSessionID domain.SessionID,
	owner *domain.SessionID,
	expected *domain.SessionID,
	reason domain.OwnershipEventReason,
	at time.Time,
) (domain.RemoteWorkerLink, bool, error) {
	if !reason.Valid() {
		return domain.RemoteWorkerLink{}, false, fmt.Errorf("invalid ownership reason %q", reason)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var link domain.RemoteWorkerLink
	changed := false
	err := s.inTx(ctx, "set remote worker owner", func(q *gen.Queries) error {
		row, err := q.GetRemoteWorkerLink(ctx, gen.GetRemoteWorkerLinkParams{
			HostID: hostID, RemoteSessionID: string(remoteSessionID),
		})
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("remote worker link %s/%s not found", hostID, remoteSessionID)
		}
		if err != nil {
			return err
		}
		current := remoteLinkFromRow(row)
		if expected != nil && !sameOwner(current.OwnerSessionID, expected) {
			link = current
			return nil
		}
		if sameOwner(current.OwnerSessionID, owner) {
			link = current
			return nil
		}
		if err := q.InsertRemoteWorkerOwnerEvent(ctx, gen.InsertRemoteWorkerOwnerEventParams{
			HostID:                 hostID,
			RemoteSessionID:        remoteSessionID,
			PreviousOwnerSessionID: current.OwnerSessionID,
			NewOwnerSessionID:      owner,
			Reason:                 reason,
			CreatedAt:              at,
		}); err != nil {
			return err
		}
		rows, err := q.SetRemoteWorkerOwner(ctx, gen.SetRemoteWorkerOwnerParams{
			OwnerSessionID:  owner,
			HostID:          hostID,
			RemoteSessionID: string(remoteSessionID),
		})
		if err != nil {
			return err
		}
		if rows != 1 {
			return fmt.Errorf("remote worker link %s/%s not found", hostID, remoteSessionID)
		}
		updated, err := q.GetRemoteWorkerLink(ctx, gen.GetRemoteWorkerLinkParams{
			HostID: hostID, RemoteSessionID: string(remoteSessionID),
		})
		if err != nil {
			return err
		}
		link = remoteLinkFromRow(updated)
		changed = true
		return nil
	})
	if err != nil {
		return domain.RemoteWorkerLink{}, false, fmt.Errorf("set remote worker owner %s/%s: %w", hostID, remoteSessionID, err)
	}
	return link, changed, nil
}

// ListRemoteWorkerOwnerEvents returns one link's ownership history in write
// order.
func (s *Store) ListRemoteWorkerOwnerEvents(
	ctx context.Context,
	hostID string,
	remoteSessionID domain.SessionID,
) ([]domain.RemoteWorkerOwnerEvent, error) {
	rows, err := s.qr.ListRemoteWorkerOwnerEvents(ctx, gen.ListRemoteWorkerOwnerEventsParams{
		HostID: hostID, RemoteSessionID: remoteSessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("list remote worker owner events %s/%s: %w", hostID, remoteSessionID, err)
	}
	events := make([]domain.RemoteWorkerOwnerEvent, len(rows))
	for i, row := range rows {
		events[i] = domain.RemoteWorkerOwnerEvent{
			ID:                     row.ID,
			HostID:                 row.HostID,
			RemoteSessionID:        row.RemoteSessionID,
			PreviousOwnerSessionID: row.PreviousOwnerSessionID,
			NewOwnerSessionID:      row.NewOwnerSessionID,
			Reason:                 row.Reason,
			CreatedAt:              row.CreatedAt,
		}
	}
	return events, nil
}

func sameOwner(a, b *domain.SessionID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
