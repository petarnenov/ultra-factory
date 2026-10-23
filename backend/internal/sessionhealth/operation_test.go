package sessionhealth

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func hibernatedChat(id domain.SessionID, hibernatedAt time.Time) domain.SessionRecord {
	return domain.SessionRecord{
		ID: id, ProjectID: "p-1", Kind: domain.KindWorker,
		Mode: domain.SessionModeChat, HibernatedAt: &hibernatedAt,
	}
}

func TestWithOperationBlocksOnlyPastThreshold(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	cfg := DefaultConfig()
	ok := domain.SessionHealth{State: domain.SessionHealthOK, Reason: "hibernated"}

	fresh := &domain.SessionAgentOperation{Kind: "resume", StartedAt: now.Add(-cfg.OperationAfter + time.Second)}
	if got := WithOperation(ok, fresh, now, cfg); got != ok {
		t.Fatalf("fresh operation changed health: %+v", got)
	}
	if got := WithOperation(ok, nil, now, cfg); got != ok {
		t.Fatalf("no operation changed health: %+v", got)
	}

	stuck := &domain.SessionAgentOperation{Kind: "resume", StartedAt: now.Add(-cfg.OperationAfter)}
	got := WithOperation(ok, stuck, now, cfg)
	if got.State != domain.SessionHealthBlocked || got.Reason != ReasonAgentOperationStuck || got.Class != "resume" {
		t.Fatalf("health = %+v, want blocked agent_operation_stuck for resume", got)
	}
	if got.SilentForSeconds != int64(cfg.OperationAfter/time.Second) {
		t.Fatalf("silent = %d, want %d", got.SilentForSeconds, int64(cfg.OperationAfter/time.Second))
	}
}

type fakeOperations struct {
	op        domain.SessionAgentOperation
	abandoned int
}

func (f *fakeOperations) AgentOperation(domain.SessionID) (domain.SessionAgentOperation, bool) {
	return f.op, !f.op.StartedAt.IsZero()
}

func (f *fakeOperations) AbandonAgentOperation(domain.SessionID) bool {
	f.abandoned++
	return true
}

func TestObserverReportsAndAbandonsStuckWakeOfHibernatedSession(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	cfg := DefaultConfig()
	store := &fakeStore{
		sessions: []domain.SessionRecord{hibernatedChat("s-1", now.Add(-time.Hour))},
		progress: map[domain.SessionID]domain.SessionProgress{},
	}
	ops := &fakeOperations{op: domain.SessionAgentOperation{Kind: "resume", StartedAt: now.Add(-cfg.OperationAfter - time.Minute)}}
	notifier := &fakeNotifier{}
	reports := 0
	observer := NewObserver(store, notifier, ObserverConfig{
		Health: cfg, Clock: func() time.Time { return now }, Operations: ops,
		OnStalled: func(_ context.Context, _ domain.SessionRecord, health domain.SessionHealth) error {
			if health.State != domain.SessionHealthBlocked || health.Reason != ReasonAgentOperationStuck {
				t.Errorf("health = %+v, want blocked agent_operation_stuck", health)
			}
			reports++
			return nil
		},
	})

	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reports != 1 || ops.abandoned != 1 || len(notifier.intents) != 1 {
		t.Fatalf("reports=%d abandoned=%d intents=%d, want 1 each", reports, ops.abandoned, len(notifier.intents))
	}
}

func TestObserverLeavesFreshOperationAlone(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	store := &fakeStore{
		sessions: []domain.SessionRecord{hibernatedChat("s-1", now.Add(-time.Hour))},
		progress: map[domain.SessionID]domain.SessionProgress{},
	}
	ops := &fakeOperations{op: domain.SessionAgentOperation{Kind: "resume", StartedAt: now.Add(-time.Second)}}
	observer := NewObserver(store, &fakeNotifier{}, ObserverConfig{
		Health: DefaultConfig(), Clock: func() time.Time { return now }, Operations: ops,
	})
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ops.abandoned != 0 {
		t.Fatalf("abandoned = %d, want 0 for a fresh operation", ops.abandoned)
	}
}
