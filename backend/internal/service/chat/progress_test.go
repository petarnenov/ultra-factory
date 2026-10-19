package chat

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/sessionhealth"
)

type fakeProgress struct {
	signals []sessionhealth.Signal
	blocked []string
}

func (f *fakeProgress) RecordProgress(_ context.Context, _ domain.SessionID, signal sessionhealth.Signal) error {
	f.signals = append(f.signals, signal)
	return nil
}

func (f *fakeProgress) RecordBlocked(_ context.Context, _ domain.SessionID, reason string, _ time.Time) error {
	f.blocked = append(f.blocked, reason)
	return nil
}

func TestChatToolBoundariesRecordNamedProgress(t *testing.T) {
	progress := &fakeProgress{}
	c := &Controller{sessionID: "s-1", progress: progress}
	now := time.Unix(10_000, 0).UTC()

	c.recordProgress(context.Background(), ports.ChatEvent{
		Kind: ports.ChatEventActivityStarted, ActivityKind: domain.ActivityKindCommand, Summary: "Bash",
	}, now)
	if len(progress.signals) != 2 {
		t.Fatalf("signals = %+v, want provider and tool start", progress.signals)
	}
	tool := progress.signals[1]
	if tool.Class != sessionhealth.ClassTool || tool.ToolName != "Bash" || tool.ToolEnd {
		t.Fatalf("tool start = %+v", tool)
	}

	progress.signals = nil
	c.recordProgress(context.Background(), ports.ChatEvent{
		Kind: ports.ChatEventActivityCompleted, ActivityKind: domain.ActivityKindMCPTool, Summary: "search",
	}, now.Add(time.Second))
	if len(progress.signals) != 2 || !progress.signals[1].ToolEnd || progress.signals[1].ToolName != "search" {
		t.Fatalf("tool end = %+v", progress.signals)
	}
}

func TestChatDeltaHeartbeatsAreThrottled(t *testing.T) {
	progress := &fakeProgress{}
	c := &Controller{sessionID: "s-1", progress: progress}
	now := time.Unix(10_000, 0).UTC()
	c.recordProgress(context.Background(), ports.ChatEvent{Kind: ports.ChatEventMessageDelta, Delta: "a"}, now)
	c.recordProgress(context.Background(), ports.ChatEvent{Kind: ports.ChatEventMessageDelta, Delta: "b"}, now.Add(time.Second))
	if len(progress.signals) != 1 {
		t.Fatalf("signals = %+v, want one throttled provider heartbeat", progress.signals)
	}
	c.recordProgress(context.Background(), ports.ChatEvent{Kind: ports.ChatEventMessageDelta, Delta: "c"}, now.Add(6*time.Second))
	if len(progress.signals) != 2 {
		t.Fatalf("signals = %+v, want a later heartbeat", progress.signals)
	}
	// A turn boundary is never throttled.
	c.recordProgress(context.Background(), ports.ChatEvent{Kind: ports.ChatEventTurnCompleted}, now.Add(7*time.Second))
	if len(progress.signals) != 3 {
		t.Fatalf("signals = %+v, want the turn boundary", progress.signals)
	}
}

func TestChatUsageIsNotProgress(t *testing.T) {
	progress := &fakeProgress{}
	c := &Controller{sessionID: "s-1", progress: progress}
	c.recordProgress(context.Background(), ports.ChatEvent{Kind: ports.ChatEventUsage}, time.Unix(10_000, 0).UTC())
	if len(progress.signals) != 0 {
		t.Fatalf("usage recorded progress: %+v", progress.signals)
	}
}

func TestChatGitCredentialFailureRecordsBlocked(t *testing.T) {
	progress := &fakeProgress{}
	c := &Controller{sessionID: "s-1", progress: progress}
	c.recordProgress(context.Background(), ports.ChatEvent{
		Kind:  ports.ChatEventCommandOutputDelta,
		Delta: "fatal: could not read Username for 'https://github.com': terminal prompts disabled",
	}, time.Unix(10_000, 0).UTC())
	if len(progress.blocked) != 1 || progress.blocked[0] != "git_credential_missing" {
		t.Fatalf("blocked = %+v", progress.blocked)
	}
}

func TestChatAuthFailureRecordsBlocked(t *testing.T) {
	progress := &fakeProgress{}
	c := &Controller{sessionID: "s-1", progress: progress}
	c.recordProgress(context.Background(), ports.ChatEvent{
		Kind: ports.ChatEventError, Err: ports.ErrChatAuthRequired,
	}, time.Unix(10_000, 0).UTC())
	if len(progress.blocked) != 1 || progress.blocked[0] != "provider_auth_required" {
		t.Fatalf("blocked = %+v", progress.blocked)
	}
}
