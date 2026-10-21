package store

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// RecordSessionCIGateEvent appends the audit row for a CI-gate flip.
func (s *Store) RecordSessionCIGateEvent(ctx context.Context, ev domain.SessionCIGateEvent) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.InsertSessionCIGateEvent(ctx, gen.InsertSessionCIGateEventParams{
		SessionID: string(ev.SessionID),
		PRURL:     ev.PRURL,
		HeadSha:   ev.HeadSHA,
		Kind:      ev.Kind,
		Reason:    ev.Reason,
		CreatedAt: ev.CreatedAt,
	}); err != nil {
		return fmt.Errorf("record ci gate event for %s: %w", ev.SessionID, err)
	}
	return nil
}

// ListSessionCIGateEvents returns a session's CI-gate flips in order.
func (s *Store) ListSessionCIGateEvents(ctx context.Context, id domain.SessionID) ([]domain.SessionCIGateEvent, error) {
	rows, err := s.qr.ListSessionCIGateEvents(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("list ci gate events for %s: %w", id, err)
	}
	out := make([]domain.SessionCIGateEvent, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.SessionCIGateEvent{
			ID: r.ID, SessionID: domain.SessionID(r.SessionID), PRURL: r.PRURL,
			HeadSHA: r.HeadSha, Kind: r.Kind, Reason: r.Reason, CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}
