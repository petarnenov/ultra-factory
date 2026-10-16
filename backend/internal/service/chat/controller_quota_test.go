package chat

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestQuotaSnapshotCallbackRetriesAndCommitsEventAfterRecovery(t *testing.T) {
	transientErr := errors.New("snapshot storage unavailable")
	var calls int
	c := &Controller{
		sessionID: "session-quota", generation: "generation-quota", harness: domain.HarnessCodex,
		conversation: domain.ConversationRecord{ID: "conversation-quota"},
		log:          slog.New(slog.DiscardHandler),
		now:          func() time.Time { return time.Unix(1, 0).UTC() },
		onCodexCapacityChanged: func(domain.SessionID, string, ports.CodexCapacityObservation) error {
			calls++
			if calls == 1 {
				return transientErr
			}
			return nil
		},
	}
	event := ports.ChatEvent{Kind: ports.ChatEventRateLimits, RateLimits: &ports.ChatRateLimits{
		CodexCapacity: &ports.CodexCapacityObservation{ObservedAt: time.Unix(1, 0).UTC(), Overall: &domain.CodexCapacityBucket{
			LimitID: "codex", Primary: &domain.CodexCapacityWindow{UsedPercent: 95},
		}},
	}}
	if err := c.apply(context.Background(), event); err != nil {
		t.Fatalf("apply after transient snapshot failure = %v", err)
	}
	if calls != 2 {
		t.Fatalf("quota callback attempts = %d, want transient failure then success", calls)
	}
}

func TestPermanentQuotaSnapshotFailureDoesNotFailProviderProjection(t *testing.T) {
	wantErr := errors.New("snapshot storage permanently unavailable")
	var calls int
	c := &Controller{
		sessionID: "session-quota", generation: "generation-quota", harness: domain.HarnessCodex,
		conversation: domain.ConversationRecord{ID: "conversation-quota"},
		log:          slog.New(slog.DiscardHandler),
		now:          time.Now,
		onCodexCapacityChanged: func(domain.SessionID, string, ports.CodexCapacityObservation) error {
			calls++
			return wantErr
		},
	}
	event := ports.ChatEvent{Kind: ports.ChatEventRateLimits, RateLimits: &ports.ChatRateLimits{
		CodexCapacity: &ports.CodexCapacityObservation{Overall: &domain.CodexCapacityBucket{
			LimitID: "codex", Primary: &domain.CodexCapacityWindow{UsedPercent: 95},
		}},
	}}
	if err := c.apply(context.Background(), event); err != nil {
		t.Fatalf("permanent snapshot error failed rate-limit projection: %v", err)
	}
	if calls != 3 {
		t.Fatalf("quota callback attempts = %d, want three bounded attempts", calls)
	}
}
