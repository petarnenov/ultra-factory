package store

import (
	"context"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// AppendSessionStage records that the session entered stage at the given time.
func (s *Store) AppendSessionStage(ctx context.Context, id domain.SessionID, stage domain.KanbanColumn, at time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.InsertSessionStageTransition(ctx, gen.InsertSessionStageTransitionParams{
		SessionID: string(id), Stage: string(stage), EnteredAt: at.UTC(),
	}); err != nil {
		return fmt.Errorf("append stage %s for %s: %w", stage, id, err)
	}
	return nil
}

// ListLatestSessionStages returns each session's most recently recorded stage.
func (s *Store) ListLatestSessionStages(ctx context.Context) (map[domain.SessionID]domain.KanbanColumn, error) {
	rows, err := s.qr.ListLatestSessionStages(ctx)
	if err != nil {
		return nil, fmt.Errorf("list latest session stages: %w", err)
	}
	out := make(map[domain.SessionID]domain.KanbanColumn, len(rows))
	for _, row := range rows {
		out[domain.SessionID(row.SessionID)] = domain.KanbanColumn(row.Stage)
	}
	return out, nil
}

// ListSessionStageTransitions returns a session's recorded stages in order.
func (s *Store) ListSessionStageTransitions(ctx context.Context, id domain.SessionID) ([]domain.StageTransition, error) {
	rows, err := s.qr.ListSessionStageTransitions(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("list stage transitions for %s: %w", id, err)
	}
	out := make([]domain.StageTransition, len(rows))
	for i, row := range rows {
		out[i] = domain.StageTransition{Stage: domain.KanbanColumn(row.Stage), EnteredAt: row.EnteredAt.UTC()}
	}
	return out, nil
}

// ListSessionConfigHistory returns a session's recorded configuration changes
// in the order they were committed.
func (s *Store) ListSessionConfigHistory(ctx context.Context, id domain.SessionID) ([]domain.ConfigChange, error) {
	rows, err := s.qr.ListSessionConfigHistory(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("list config history for %s: %w", id, err)
	}
	out := make([]domain.ConfigChange, len(rows))
	for i, row := range rows {
		out[i] = domain.ConfigChange{
			Source: domain.ConfigChangeSource(row.Source), Harness: row.Harness,
			Model: row.Model, Effort: row.Effort, RecordedAt: row.RecordedAt.UTC(),
		}
	}
	return out, nil
}

// ListSessionUsageRuns returns a session's usage grouped by run and model.
func (s *Store) ListSessionUsageRuns(ctx context.Context, id domain.SessionID) ([]domain.UsageRun, error) {
	rows, err := s.qr.ListSessionUsageRuns(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list usage runs for %s: %w", id, err)
	}
	out := make([]domain.UsageRun, len(rows))
	for i, row := range rows {
		out[i] = domain.UsageRun{
			BindingID: row.BindingID, Harness: row.Harness, NativeRootID: row.NativeRootID,
			BindingState: row.BindingState, ModelID: row.ModelID, EventCount: row.EventCount,
			InputTokens: row.InputTokens, KnownInputCount: row.KnownInputCount,
			CachedInputTokens: row.CachedInputTokens, KnownCachedInputCount: row.KnownCachedInputCount,
			OutputTokens: row.OutputTokens, KnownOutputCount: row.KnownOutputCount,
		}
	}
	return out, nil
}
