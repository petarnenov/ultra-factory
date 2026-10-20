package store

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// RecordSessionPRReadyEvent appends the audit row for a manual draft/ready flip.
func (s *Store) RecordSessionPRReadyEvent(ctx context.Context, ev domain.SessionPRReadyEvent) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.InsertSessionPRReadyEvent(ctx, gen.InsertSessionPRReadyEventParams{
		SessionID:      ev.SessionID,
		ActorSessionID: ev.ActorSessionID,
		PRURL:          ev.PRURL,
		HeadSha:        ev.HeadSHA,
		Ready:          ev.Ready,
		CreatedAt:      ev.CreatedAt,
	}); err != nil {
		return fmt.Errorf("record pr ready event for %s: %w", ev.SessionID, err)
	}
	return nil
}

// ListSessionPRReadyEvents returns a session's manual draft/ready flips in order.
func (s *Store) ListSessionPRReadyEvents(ctx context.Context, id domain.SessionID) ([]domain.SessionPRReadyEvent, error) {
	rows, err := s.qr.ListSessionPRReadyEvents(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list pr ready events for %s: %w", id, err)
	}
	out := make([]domain.SessionPRReadyEvent, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.SessionPRReadyEvent{
			ID: r.ID, SessionID: r.SessionID, ActorSessionID: r.ActorSessionID,
			PRURL: r.PRURL, HeadSHA: r.HeadSha, Ready: r.Ready, CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}
