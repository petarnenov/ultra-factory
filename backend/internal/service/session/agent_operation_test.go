package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

type agentOperationCommander struct {
	fakeCommander
	op   sessionmanager.AgentOperationStatus
	busy bool
}

func (c *agentOperationCommander) AgentOperationInProgress(domain.SessionID) (sessionmanager.AgentOperationStatus, bool) {
	return c.op, c.busy
}

// A session held by an in-flight wake must say so, not read as an idle
// hibernated session with nothing in progress.
func TestGetReportsInFlightAgentOperation(t *testing.T) {
	started := time.Now().Add(-42 * time.Second).Truncate(time.Second)
	manager := &agentOperationCommander{
		op:   sessionmanager.AgentOperationStatus{Kind: "resume", StartedAt: started, RequestID: "wake-1"},
		busy: true,
	}
	st := newFakeStore()
	st.sessions["s1"] = domain.SessionRecord{ID: "s1", ProjectID: "p1"}
	svc := NewWithDeps(Deps{Manager: manager, Store: st})

	session, err := svc.Get(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if session.AgentOperation == nil || session.AgentOperation.Kind != "resume" || session.AgentOperation.RequestID != "wake-1" ||
		!session.AgentOperation.StartedAt.Equal(started) {
		t.Fatalf("agent operation = %+v, want resume since %s", session.AgentOperation, started)
	}

	manager.busy = false
	session, err = svc.Get(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if session.AgentOperation != nil {
		t.Fatalf("idle session reported operation %+v", session.AgentOperation)
	}
}

// Busy refusals keep their holder-specific code, and the message carries the
// holder and its age so the caller can tell what is blocking the session.
func TestBusyGateRefusalsReachTheAPIWithHolderDetail(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{fmt.Errorf("kill s1: %w", sessionmanager.ErrSwitchInProgress), "AGENT_SWITCH_IN_PROGRESS"},
		{fmt.Errorf("exit agent s1: %w", sessionmanager.ErrAgentExitInProgress), "AGENT_EXIT_IN_PROGRESS"},
		{fmt.Errorf("resume agent s1: %w", sessionmanager.ErrResumeInProgress), "AGENT_RESUME_IN_PROGRESS"},
		{fmt.Errorf("kill s1: %w", sessionmanager.ErrAgentOperationInProgress), "AGENT_OPERATION_IN_PROGRESS"},
	}
	for _, tc := range cases {
		var apiErr *apierr.Error
		if !errors.As(toAPIError(tc.err), &apiErr) || apiErr.Code != tc.code {
			t.Fatalf("toAPIError(%v) = %v, want code %s", tc.err, toAPIError(tc.err), tc.code)
		}
		if !strings.Contains(apiErr.Message, tc.err.Error()) {
			t.Fatalf("message %q dropped the detail of %q", apiErr.Message, tc.err)
		}
	}
}
