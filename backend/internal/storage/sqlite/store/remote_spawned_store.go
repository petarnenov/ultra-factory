package store

import (
	"context"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// MarkRemoteSpawned records that another machine started this session with
// `ao spawn --host`. Marking a session twice is a no-op.
func (s *Store) MarkRemoteSpawned(ctx context.Context, id domain.SessionID, at time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.InsertRemoteSpawnedSession(ctx, gen.InsertRemoteSpawnedSessionParams{
		SessionID: string(id), CreatedAt: at,
	}); err != nil {
		return fmt.Errorf("mark remote spawned session %s: %w", id, err)
	}
	return nil
}

// ListActiveRemoteSpawnedSessions returns the remotely started sessions that
// are not terminated, oldest first.
func (s *Store) ListActiveRemoteSpawnedSessions(ctx context.Context) ([]domain.SessionID, error) {
	ids, err := s.qr.ListActiveRemoteSpawnedSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active remote spawned sessions: %w", err)
	}
	return ids, nil
}
