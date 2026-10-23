package chat_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

// scriptedNudgeGate answers the check from a flag flipped while a nudge waits.
type scriptedNudgeGate struct {
	resolved atomic.Bool
	checked  atomic.Int32
}

func (g *scriptedNudgeGate) ReviewNudgeResolved(context.Context, domain.ReviewNudgeToken) (bool, error) {
	g.checked.Add(1)
	return g.resolved.Load(), nil
}

var reviewNudgeID = domain.ReviewNudgeClientMessageID(
	domain.ReviewNudgeToken{PRURL: "https://github.com/o/r/pull/7", ThreadID: "T1"}, "n1")

func sendRunningRootTurn(t *testing.T, h *harness) {
	t.Helper()
	if _, err := h.svc.Send(context.Background(), testSession, ports.ChatUserMessage{
		Text: "root", ClientMessageID: "c1", Origin: domain.MessageOriginHuman,
	}); err != nil {
		t.Fatalf("send root: %v", err)
	}
	h.conv.emit(ports.ChatEvent{Kind: ports.ChatEventTurnStarted, ProviderTurnID: "provider-turn-1"})
	h.awaitSnapshot(t, func(s store.ConversationSnapshot) bool {
		return turnStateByText(t, s)["root"] == domain.TurnStateRunning
	})
}

func queueReviewNudge(t *testing.T, h *harness) {
	t.Helper()
	if _, err := h.svc.Send(context.Background(), testSession, ports.ChatUserMessage{
		Text: "fix this thread", ClientMessageID: reviewNudgeID, Origin: domain.MessageOriginAutomation,
	}); err != nil {
		t.Fatalf("queue nudge: %v", err)
	}
}

func completeRootTurn(t *testing.T, h *harness) {
	t.Helper()
	h.conv.emit(ports.ChatEvent{
		Kind: ports.ChatEventTurnCompleted, ProviderTurnID: "provider-turn-1",
		TurnState: domain.TurnStateCompleted,
	})
}

func TestQueuedReviewNudgeResolvedWhileQueuedIsDroppedAtDrain(t *testing.T) {
	h := newHarness(t)
	sendRunningRootTurn(t, h)
	queueReviewNudge(t, h)

	h.nudges.resolved.Store(true)
	completeRootTurn(t, h)

	h.awaitSnapshot(t, func(s store.ConversationSnapshot) bool {
		return countTurns(s, domain.TurnStateCancelled) == 1
	})
	if got := h.conv.sentTexts(); len(got) != 1 || got[0] != "root" {
		t.Fatalf("provider received %v, want only the root prompt", got)
	}
}

func TestQueuedReviewNudgeStillUnresolvedIsDelivered(t *testing.T) {
	h := newHarness(t)
	sendRunningRootTurn(t, h)
	queueReviewNudge(t, h)

	completeRootTurn(t, h)

	h.awaitSnapshot(t, func(s store.ConversationSnapshot) bool {
		return countTurns(s, domain.TurnStateRunning) == 1 && countTurns(s, domain.TurnStateQueued) == 0
	})
	if got := h.conv.sentTexts(); len(got) != 2 || got[1] != "fix this thread" {
		t.Fatalf("provider received %v, want the root prompt then the bare nudge", got)
	}
}

func TestReviewNudgeResolvedBeforeIdleDispatchIsNotSent(t *testing.T) {
	h := newHarness(t)
	h.nudges.resolved.Store(true)

	turn, err := h.svc.Send(context.Background(), testSession, ports.ChatUserMessage{
		Text: "fix this thread", ClientMessageID: reviewNudgeID, Origin: domain.MessageOriginAutomation,
	})

	if err != nil || turn.ID != "" {
		t.Fatalf("Send = %+v, %v; want no turn", turn, err)
	}
	if got := h.conv.sentTexts(); len(got) != 0 {
		t.Fatalf("provider received %v for a resolved thread", got)
	}
}

func countTurns(s store.ConversationSnapshot, state domain.TurnState) int {
	count := 0
	for _, turn := range s.Turns {
		if turn.State == state {
			count++
		}
	}
	return count
}
