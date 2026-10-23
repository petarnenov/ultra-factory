package sessionmanager

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type scriptedNudgeGate struct {
	resolved bool
	err      error
	checks   []domain.ReviewNudgeToken
}

func (g *scriptedNudgeGate) ReviewNudgeResolved(_ context.Context, token domain.ReviewNudgeToken) (bool, error) {
	g.checks = append(g.checks, token)
	return g.resolved, g.err
}

var queuedNudgeToken = domain.ReviewNudgeToken{PRURL: "https://github.com/o/r/pull/7", ThreadID: "T1"}

// queueNudgeForReplay leaves one review nudge in a completed transition outbox.
func queueNudgeForReplay(t *testing.T, store *transitionStore) {
	t.Helper()
	now := time.Now()
	store.transitions["transition-1"] = domain.SessionInterfaceTransition{
		ID: "transition-1", SessionID: "session-1",
		SourceMode: domain.SessionModeChat, TargetMode: domain.SessionModeTUI,
		Policy:    domain.SessionInterfaceTransitionDrain,
		Phase:     domain.SessionInterfaceTransitionCompleted,
		CreatedAt: now, UpdatedAt: now, CompletedAt: now,
	}
	if err := store.EnqueueSessionInterfaceTransitionMessage(
		context.Background(), "transition-1", domain.ReviewNudgeClientMessageID(queuedNudgeToken, "n1"),
		"fix this thread", now, ports.MessageDeliveryOptions{},
	); err != nil {
		t.Fatal(err)
	}
}

func TestSendQueuesReviewNudgeDuringTransitionAsTokenAndBareText(t *testing.T) {
	manager, store, _, _, _ := newTransitionManager(t, domain.SessionModeTUI)
	now := time.Now()
	store.transitions["transition-1"] = domain.SessionInterfaceTransition{
		ID: "transition-1", SessionID: "session-1", SourceMode: domain.SessionModeTUI,
		TargetMode: domain.SessionModeChat, Policy: domain.SessionInterfaceTransitionDrain,
		Phase: domain.SessionInterfaceTransitionDraining, CreatedAt: now, UpdatedAt: now,
	}

	err := manager.Send(context.Background(), "session-1",
		domain.WrapReviewNudge(queuedNudgeToken, "fix this thread"), nil)

	if err != nil {
		t.Fatal(err)
	}
	messages, _ := store.ListPendingSessionInterfaceTransitionMessages(context.Background(), "transition-1")
	if len(messages) != 1 || messages[0].Message != "fix this thread" {
		t.Fatalf("queued messages = %+v, want one bare nudge", messages)
	}
	if token, ok := domain.ParseReviewNudgeClientMessageID(messages[0].ClientMessageID); !ok || token != queuedNudgeToken {
		t.Fatalf("queued id %q does not carry the thread token", messages[0].ClientMessageID)
	}
}

func TestTransitionReplayDropsReviewNudgeResolvedWhileQueued(t *testing.T) {
	manager, store, _, _, _ := newTransitionManager(t, domain.SessionModeTUI)
	gate := &scriptedNudgeGate{resolved: true}
	manager.reviewNudges = gate
	queueNudgeForReplay(t, store)

	if err := manager.deliverAllTransitionMessages(context.Background()); err != nil {
		t.Fatalf("replay: %v", err)
	}

	if len(store.messenger.msgs) != 0 {
		t.Fatalf("resolved thread was replayed to the worker: %v", store.messenger.msgs)
	}
	if len(gate.checks) != 1 || gate.checks[0] != queuedNudgeToken {
		t.Fatalf("freshness checks = %+v, want one for %+v", gate.checks, queuedNudgeToken)
	}
	if pending, _ := store.ListPendingSessionInterfaceTransitionMessages(context.Background(), "transition-1"); len(pending) != 0 {
		t.Fatalf("dropped nudge stays queued: %+v", pending)
	}
}

func TestTransitionReplayDeliversReviewNudgeStillUnresolved(t *testing.T) {
	manager, store, _, _, _ := newTransitionManager(t, domain.SessionModeTUI)
	manager.reviewNudges = &scriptedNudgeGate{}
	queueNudgeForReplay(t, store)

	if err := manager.deliverAllTransitionMessages(context.Background()); err != nil {
		t.Fatalf("replay: %v", err)
	}

	if len(store.messenger.msgs) != 1 {
		t.Fatalf("delivered %v, want the nudge once", store.messenger.msgs)
	}
}
