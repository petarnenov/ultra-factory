package lifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type progressFakeStore struct {
	*fakeStore
	updates []domain.SessionProgressUpdate
}

func (s *progressFakeStore) RecordSessionProgress(_ context.Context, _ domain.SessionID, update domain.SessionProgressUpdate) error {
	s.updates = append(s.updates, update)
	return nil
}

func TestHookToolBoundariesRecordNamedProgress(t *testing.T) {
	store := &progressFakeStore{fakeStore: newFakeStore()}
	m := New(store, nil)
	now := time.Unix(10_000, 0).UTC()

	m.recordActivityProgress(context.Background(), "s-1", ports.ActivitySignal{
		Valid: true, State: domain.ActivityActive, Event: "pre-tool-use",
		ToolName: "Bash", ToolUseID: "tool-1", Timestamp: now,
	}, now)
	if len(store.updates) != 2 {
		t.Fatalf("updates = %+v, want provider and tool start", store.updates)
	}
	var start domain.SessionProgressUpdate
	for _, update := range store.updates {
		if update.InFlightTool != nil {
			start = update
		}
	}
	if start.InFlightTool == nil || *start.InFlightTool != "Bash" || start.ToolAt.IsZero() {
		t.Fatalf("tool start = %+v", start)
	}

	store.updates = nil
	m.recordActivityProgress(context.Background(), "s-1", ports.ActivitySignal{
		Valid: true, State: domain.ActivityActive, Event: "post-tool-use",
		ToolName: "Bash", ToolUseID: "tool-1", Timestamp: now.Add(time.Second),
	}, now.Add(time.Second))
	cleared := false
	for _, update := range store.updates {
		cleared = cleared || update.ClearInFlight
	}
	if len(store.updates) != 2 || !cleared {
		t.Fatalf("updates = %+v, want a tool end", store.updates)
	}
}

type rejectedProgressStore struct {
	*progressFakeStore
}

func (s *rejectedProgressStore) UpdateSessionFromActivitySignal(context.Context, domain.SessionRecord, int64) (bool, error) {
	return false, nil
}

func TestSameStateHookStillRecordsToolProgress(t *testing.T) {
	store := &progressFakeStore{fakeStore: newFakeStore()}
	rec := working("s-1")
	rec.FirstSignalAt = time.Unix(9_000, 0).UTC()
	store.sessions["s-1"] = rec
	m := New(store, nil)
	now := time.Unix(10_000, 0).UTC()

	if err := m.ApplyActivitySignal(context.Background(), "s-1", ports.ActivitySignal{
		Valid: true, State: domain.ActivityActive, Event: "pre-tool-use",
		ToolName: "Bash", ToolUseID: "tool-1", Timestamp: now,
	}); err != nil {
		t.Fatal(err)
	}
	if len(store.updates) != 2 {
		t.Fatalf("updates = %+v, want provider and tool start", store.updates)
	}
	if store.updates[0].InFlightTool == nil || *store.updates[0].InFlightTool != "Bash" {
		t.Fatalf("tool start = %+v", store.updates[0])
	}

	store.updates = nil
	if err := m.ApplyActivitySignal(context.Background(), "s-1", ports.ActivitySignal{
		Valid: true, State: domain.ActivityActive, Event: "post-tool-use",
		ToolName: "Bash", ToolUseID: "tool-1", Timestamp: now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	cleared := false
	for _, update := range store.updates {
		cleared = cleared || update.ClearInFlight
	}
	if !cleared {
		t.Fatalf("post-tool-use did not clear the in-flight tool: %+v", store.updates)
	}
}

func TestRejectedActivitySignalDoesNotRecordProgress(t *testing.T) {
	store := &rejectedProgressStore{progressFakeStore: &progressFakeStore{fakeStore: newFakeStore()}}
	rec := working("s-1")
	rec.Activity = domain.Activity{State: domain.ActivityIdle, LastActivityAt: time.Unix(9_000, 0).UTC()}
	store.sessions["s-1"] = rec
	m := New(store, nil)
	if err := m.ApplyActivitySignal(context.Background(), "s-1", ports.ActivitySignal{
		Valid: true, State: domain.ActivityActive, Event: "pre-tool-use",
		ToolName: "Bash", ToolUseID: "tool-1",
	}); err != nil {
		t.Fatal(err)
	}
	if len(store.updates) != 0 {
		t.Fatalf("rejected signal recorded progress: %+v", store.updates)
	}
}

func TestHookStateOnlySignalsAreNotProgress(t *testing.T) {
	store := &progressFakeStore{fakeStore: newFakeStore()}
	m := New(store, nil)
	now := time.Unix(10_000, 0).UTC()
	m.recordActivityProgress(context.Background(), "s-1", ports.ActivitySignal{
		Valid: true, State: domain.ActivityWaitingInput, Event: "notification", Timestamp: now,
	}, now)
	if len(store.updates) != 0 {
		t.Fatalf("notification recorded progress: %+v", store.updates)
	}
}
