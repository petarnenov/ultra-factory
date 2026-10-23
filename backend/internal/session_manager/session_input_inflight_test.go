package sessionmanager

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"

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

func TestKillReportsWakeThatIgnoresCancelWithoutClearingGate(t *testing.T) {
	ctx := context.WithValue(context.Background(), middleware.RequestIDKey, "wake-starter-19")
	id := domain.SessionID("worker-1")
	logs := &bytes.Buffer{}
	m := newInFlightWakeTestManager(logs)

	// A wake that ignores cancellation keeps the gate: clearing it would let
	// kill tear down while the wake is still relaunching the agent.
	if err := m.beginAgentOperation(ctx, id, agentOperationResume); err != nil {
		t.Fatal(err)
	}
	m.setAgentOperationCancel(id, func() {})

	_, err := m.kill(ctx, id, false)
	if !errors.Is(err, ErrResumeInProgress) || !strings.Contains(err.Error(), "resume in progress for") ||
		!strings.Contains(err.Error(), "wake-starter-19") {
		t.Fatalf("kill over wedged wake = %v, want ErrResumeInProgress with wake details", err)
	}
	if !m.SessionMutationInProgress(id) {
		t.Fatal("gate was cleared while the wake was still running")
	}
	if _, err := m.ExitAgent(ctx, id); !errors.Is(err, ErrResumeInProgress) {
		t.Fatalf("exit over wedged wake = %v, want ErrResumeInProgress", err)
	}
	if !m.SessionMutationInProgress(id) {
		t.Fatal("exit cleared the resume marker")
	}
	m.endAgentResume(id)
}

func TestBusyGateErrorNamesOperationAndAge(t *testing.T) {
	ctx := context.WithValue(context.Background(), middleware.RequestIDKey, "starter-req-17")
	id := domain.SessionID("worker-1")
	m := newInFlightWakeTestManager(&bytes.Buffer{})
	if err := m.beginAgentOperation(ctx, id, agentOperationRestore); err != nil {
		t.Fatal(err)
	}
	_, err := m.kill(ctx, id, false)
	if !errors.Is(err, ErrAgentOperationInProgress) {
		t.Fatalf("kill during restore = %v, want ErrAgentOperationInProgress", err)
	}
	if !strings.Contains(err.Error(), "restore in progress for") || !strings.Contains(err.Error(), "starter-req-17") {
		t.Fatalf("error %q does not name the operation, age, and starter request ID", err)
	}
}

func TestAgentOperationInProgressReportsKindAndStart(t *testing.T) {
	ctx := context.WithValue(context.Background(), middleware.RequestIDKey, "wake-req-18")
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
	if !ok || status.Kind != string(agentOperationResume) || status.StartedAt.Before(before) || status.RequestID != "wake-req-18" {
		t.Fatalf("status = %+v ok=%v, want resume started after %v", status, ok, before)
	}
	m.endAgentResume(id)
	if _, ok := m.AgentOperationInProgress(id); ok {
		t.Fatal("released operation still reported")
	}
}

func TestAbandonCancelsSwitchAndExitWithoutReleasingTheirFence(t *testing.T) {
	for _, kind := range []agentOperationKind{agentOperationSwitch, agentOperationExit} {
		t.Run(string(kind), func(t *testing.T) {
			m := newInFlightWakeTestManager(&bytes.Buffer{})
			id := domain.SessionID("worker-1")
			if err := m.beginAgentOperation(context.Background(), id, kind); err != nil {
				t.Fatal(err)
			}
			opCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			m.setAgentOperationCancel(id, cancel)
			if !m.AbandonAgentOperation(id) || opCtx.Err() == nil {
				t.Fatal("abandon did not cancel the transition")
			}
			if !m.SessionMutationInProgress(id) {
				t.Fatal("abandon released the fence before the operation unwound")
			}
			m.endAgentOperation(id, kind)
			if m.SessionMutationInProgress(id) {
				t.Fatal("completed transition kept its fence")
			}
		})
	}
}

// Kill can land after the wake takes the marker and before it registers its
// cancel. Kill must still stop the wake rather than wait out the full bound.
func TestKillBeforeWakeRegistersCancelStillStopsIt(t *testing.T) {
	ctx := context.Background()
	id := domain.SessionID("worker-1")
	logs := &bytes.Buffer{}
	m := newInFlightWakeTestManager(logs)
	m.resumePreemptWait = 5 * time.Second

	if err := m.beginAgentOperation(ctx, id, agentOperationResume); err != nil {
		t.Fatal(err)
	}
	wakeCtx, cancelWake := context.WithCancel(ctx)
	defer cancelWake()
	released := make(chan struct{})
	go func() {
		<-wakeCtx.Done()
		m.endAgentResume(id)
		close(released)
	}()

	killed := make(chan error, 1)
	go func() {
		_, err := m.kill(ctx, id, false)
		killed <- err
	}()
	// Register the cancel only after kill has already started waiting.
	time.Sleep(100 * time.Millisecond)
	m.setAgentOperationCancel(id, cancelWake)

	select {
	case err := <-killed:
		if err != nil {
			t.Fatalf("kill = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("kill waited out the preemption bound instead of stopping the wake")
	}
	<-released
	if strings.Contains(logs.String(), "cleared stale resume marker") {
		t.Fatalf("wake was force-cleared although it was cancelled: %s", logs.String())
	}
}

func TestAbandonAgentOperationCancelsOnlyCancellableHolder(t *testing.T) {
	ctx := context.Background()
	id := domain.SessionID("worker-1")
	m := newInFlightWakeTestManager(&bytes.Buffer{})
	if m.AbandonAgentOperation(id) {
		t.Fatal("idle session reported an abandoned operation")
	}
	if err := m.beginAgentOperation(ctx, id, agentOperationResume); err != nil {
		t.Fatal(err)
	}
	if m.AbandonAgentOperation(id) {
		t.Fatal("wake without a registered cancel reported abandoned")
	}
	wakeCtx, cancel := context.WithCancel(ctx)
	m.setAgentOperationCancel(id, cancel)
	if !m.AbandonAgentOperation(id) {
		t.Fatal("wake with a cancel was not abandoned")
	}
	if wakeCtx.Err() == nil {
		t.Fatal("abandon did not cancel the wake context")
	}
}
