package sessionmanager

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// notFoundStore answers the one store read kill and exit reach before any
// teardown, so the tests can observe the gate without a full session fixture.
type notFoundStore struct{ Store }

func (notFoundStore) GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error) {
	return domain.SessionRecord{}, false, nil
}

func newInFlightWakeTestManager(logs *bytes.Buffer) *Manager {
	m := newInputLeaseTestManager()
	m.store = notFoundStore{}
	m.asyncChatSpawns = make(map[domain.SessionID]*asyncChatSpawnRun)
	m.logger = slog.New(slog.NewTextHandler(logs, nil))
	m.resumePreemptWait = 50 * time.Millisecond
	return m
}

// A background wake holds the gate as a resume. A user kill or exit-agent
// must not be refused with a switch or exit error that names nothing running.
func TestExplicitControlDuringInFlightWakeIsNotRefusedAsSwitchOrExit(t *testing.T) {
	ctx := context.Background()
	id := domain.SessionID("worker-1")

	t.Run("kill", func(t *testing.T) {
		m := newInFlightWakeTestManager(&bytes.Buffer{})
		if err := m.beginAgentOperation(ctx, id, agentOperationResume); err != nil {
			t.Fatal(err)
		}
		_, err := m.kill(ctx, id, false)
		if errors.Is(err, ErrSwitchInProgress) {
			t.Fatalf("kill during wake = %v; a wake must not be reported as a switch", err)
		}
	})

	t.Run("exit", func(t *testing.T) {
		m := newInFlightWakeTestManager(&bytes.Buffer{})
		if err := m.beginAgentOperation(ctx, id, agentOperationResume); err != nil {
			t.Fatal(err)
		}
		_, err := m.ExitAgent(ctx, id)
		if errors.Is(err, ErrAgentExitInProgress) {
			t.Fatalf("exit during wake = %v; a wake must not be reported as an exit", err)
		}
	})
}

func TestKillCancelsWakeAndReleasesGate(t *testing.T) {
	ctx := context.Background()
	id := domain.SessionID("worker-1")
	logs := &bytes.Buffer{}
	m := newInFlightWakeTestManager(logs)

	wakeCtx, cancelWake := context.WithCancel(ctx)
	defer cancelWake()
	if err := m.beginAgentOperation(ctx, id, agentOperationResume); err != nil {
		t.Fatal(err)
	}
	m.setAgentOperationCancel(id, cancelWake)
	released := make(chan struct{})
	go func() {
		<-wakeCtx.Done()
		// The wake unwinds and releases its marker, as ResumeAgentWithMode does.
		m.endAgentResume(id)
		close(released)
	}()

	if _, err := m.kill(ctx, id, false); err != nil {
		t.Fatalf("kill after cancelling wake = %v, want nil", err)
	}
	<-released
	if m.SessionMutationInProgress(id) {
		t.Fatal("gate still held after kill")
	}
}

func TestKillClearsStaleWakeMarkerWithLoggedReason(t *testing.T) {
	ctx := context.Background()
	id := domain.SessionID("worker-1")
	logs := &bytes.Buffer{}
	m := newInFlightWakeTestManager(logs)

	// A wake that ignores cancellation must not pin the session forever.
	if err := m.beginAgentOperation(ctx, id, agentOperationResume); err != nil {
		t.Fatal(err)
	}
	m.setAgentOperationCancel(id, func() {})

	if _, err := m.kill(ctx, id, false); err != nil {
		t.Fatalf("kill over stale wake = %v, want nil", err)
	}
	if !strings.Contains(logs.String(), "cleared stale resume marker") {
		t.Fatalf("log = %q, want a stale-marker reason", logs.String())
	}
}

func TestBusyGateErrorNamesOperationAndAge(t *testing.T) {
	ctx := context.Background()
	id := domain.SessionID("worker-1")
	m := newInFlightWakeTestManager(&bytes.Buffer{})
	if err := m.beginAgentOperation(ctx, id, agentOperationRestore); err != nil {
		t.Fatal(err)
	}
	_, err := m.kill(ctx, id, false)
	if !errors.Is(err, ErrAgentOperationInProgress) {
		t.Fatalf("kill during restore = %v, want ErrAgentOperationInProgress", err)
	}
	if !strings.Contains(err.Error(), "restore in progress for") {
		t.Fatalf("error %q does not name the operation and its age", err)
	}
}

func TestAgentOperationInProgressReportsKindAndStart(t *testing.T) {
	ctx := context.Background()
	id := domain.SessionID("worker-1")
	m := newInFlightWakeTestManager(&bytes.Buffer{})
	if _, ok := m.AgentOperationInProgress(id); ok {
		t.Fatal("idle session reported an operation")
	}
	before := time.Now()
	if err := m.beginAgentOperation(ctx, id, agentOperationResume); err != nil {
		t.Fatal(err)
	}
	status, ok := m.AgentOperationInProgress(id)
	if !ok || status.Kind != string(agentOperationResume) || status.StartedAt.Before(before) {
		t.Fatalf("status = %+v ok=%v, want resume started after %v", status, ok, before)
	}
	m.endAgentResume(id)
	if _, ok := m.AgentOperationInProgress(id); ok {
		t.Fatal("released operation still reported")
	}
}
