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

// GetSessionProgress returns the durable progress facts for one session.
func (s *Store) GetSessionProgress(ctx context.Context, id domain.SessionID) (domain.SessionProgress, bool, error) {
	row, err := s.qr.GetSessionProgress(ctx, string(id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SessionProgress{}, false, nil
	}
	if err != nil {
		return domain.SessionProgress{}, false, fmt.Errorf("get session progress %s: %w", id, err)
	}
	return sessionProgressFromRow(row), true, nil
}

// ListSessionProgress returns every durable progress row keyed by session.
func (s *Store) ListSessionProgress(ctx context.Context) (map[domain.SessionID]domain.SessionProgress, error) {
	rows, err := s.qr.ListSessionProgress(ctx)
	if err != nil {
		return nil, fmt.Errorf("list session progress: %w", err)
	}
	out := make(map[domain.SessionID]domain.SessionProgress, len(rows))
	for _, row := range rows {
		out[domain.SessionID(row.SessionID)] = sessionProgressFromRow(row)
	}
	return out, nil
}

// RecordSessionProgress merges one partial update under the writer lock so
// two signal classes cannot lose each other's timestamp.
func (s *Store) RecordSessionProgress(ctx context.Context, id domain.SessionID, update domain.SessionProgressUpdate) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	current := domain.SessionProgress{}
	row, err := s.qw.GetSessionProgress(ctx, string(id))
	switch {
	case err == nil:
		current = sessionProgressFromRow(row)
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("read session progress %s: %w", id, err)
	}
	next := mergeSessionProgress(current, update)
	if err := s.qw.UpsertSessionProgress(ctx, gen.UpsertSessionProgressParams{
		SessionID:              string(id),
		ProviderAt:             timeToNullTime(next.ProviderAt),
		ToolAt:                 timeToNullTime(next.ToolAt),
		TerminalAt:             timeToNullTime(next.TerminalAt),
		GitAt:                  timeToNullTime(next.GitAt),
		InFlightTool:           next.InFlightTool,
		InFlightToolAt:         timeToNullTime(next.InFlightToolAt),
		BlockedAt:              timeToNullTime(next.BlockedAt),
		BlockedReason:          next.BlockedReason,
		LastNotifiedProgressAt: timeToNullTime(next.LastNotifiedProgressAt),
		StallCount:             int64(next.StallCount),
		LastStallAt:            timeToNullTime(next.LastStallAt),
		LastStallForSeconds:    next.LastStallForSeconds,
	}); err != nil {
		return fmt.Errorf("write session progress %s: %w", id, err)
	}
	return nil
}

func mergeSessionProgress(current domain.SessionProgress, update domain.SessionProgressUpdate) domain.SessionProgress {
	next := current
	next.ProviderAt = laterTime(next.ProviderAt, update.ProviderAt)
	next.ToolAt = laterTime(next.ToolAt, update.ToolAt)
	next.TerminalAt = laterTime(next.TerminalAt, update.TerminalAt)
	next.GitAt = laterTime(next.GitAt, update.GitAt)
	next.InFlightToolAt = laterTime(next.InFlightToolAt, update.InFlightToolAt)
	if update.ClearInFlight {
		next.InFlightTool = ""
		next.InFlightToolAt = time.Time{}
	} else if update.InFlightTool != nil {
		next.InFlightTool = *update.InFlightTool
	}
	progressAfterBlock := !next.BlockedAt.IsZero() &&
		(update.ProviderAt.After(next.BlockedAt) || update.ToolAt.After(next.BlockedAt) ||
			update.TerminalAt.After(next.BlockedAt) || update.GitAt.After(next.BlockedAt))
	if update.ClearBlocked || progressAfterBlock {
		next.BlockedAt = time.Time{}
		next.BlockedReason = ""
	} else if update.BlockedReason != nil {
		next.BlockedReason = *update.BlockedReason
		next.BlockedAt = laterTime(next.BlockedAt, update.BlockedAt)
	}
	if update.ClearLastNotified {
		next.LastNotifiedProgressAt = time.Time{}
	} else {
		next.LastNotifiedProgressAt = laterTime(next.LastNotifiedProgressAt, update.LastNotifiedFor)
	}
	if update.StallCountDelta > 0 {
		next.StallCount += update.StallCountDelta
	}
	next.LastStallAt = laterTime(next.LastStallAt, update.LastStallAt)
	if update.LastStallFor > 0 {
		next.LastStallForSeconds = int64(update.LastStallFor / time.Second)
	}
	return next
}

func laterTime(current, candidate time.Time) time.Time {
	if candidate.After(current) {
		return candidate.UTC()
	}
	return current
}

func sessionProgressFromRow(row gen.SessionProgress) domain.SessionProgress {
	return domain.SessionProgress{
		ProviderAt:             nullTimeToTime(row.ProviderAt),
		ToolAt:                 nullTimeToTime(row.ToolAt),
		TerminalAt:             nullTimeToTime(row.TerminalAt),
		GitAt:                  nullTimeToTime(row.GitAt),
		InFlightTool:           row.InFlightTool,
		InFlightToolAt:         nullTimeToTime(row.InFlightToolAt),
		BlockedAt:              nullTimeToTime(row.BlockedAt),
		BlockedReason:          row.BlockedReason,
		LastNotifiedProgressAt: nullTimeToTime(row.LastNotifiedProgressAt),
		StallCount:             int(row.StallCount),
		LastStallAt:            nullTimeToTime(row.LastStallAt),
		LastStallForSeconds:    row.LastStallForSeconds,
	}
}
