package remoteworker

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// recordingNotifier keeps every notice in delivery order.
type recordingNotifier struct {
	ids      []domain.SessionID
	messages []string
}

func (n *recordingNotifier) Send(_ context.Context, id domain.SessionID, message string, _ *ports.SpawnAttachment) error {
	n.ids = append(n.ids, id)
	n.messages = append(n.messages, message)
	return nil
}

func (n *recordingNotifier) to(id domain.SessionID) []string {
	var out []string
	for i, got := range n.ids {
		if got == id {
			out = append(out, n.messages[i])
		}
	}
	return out
}

func TestAssignOwnerOnARemoteLinkNoticesCoordinators(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	store := &fakeStore{
		sessions: map[domain.SessionID]domain.SessionRecord{
			"ao-1": {ID: "ao-1", ProjectID: "ultra-factory", Kind: domain.KindOrchestrator},
			"ao-2": {ID: "ao-2", ProjectID: "ultra-factory", Kind: domain.KindCoordinator},
			"ao-3": {ID: "ao-3", ProjectID: "ultra-factory", Kind: domain.KindCoordinator},
		},
		stored: []domain.RemoteWorkerLink{{
			HostID: "host-build", RemoteSessionID: "widgets-3", RemoteProjectID: "widgets",
			ProjectID: "ultra-factory", CreatedAt: now,
		}},
	}
	notes := &recordingNotifier{}
	svc := New(Deps{Store: store, Now: func() time.Time { return now }, Notifier: notes})
	ctx := context.Background()

	if _, err := svc.AssignOwner(ctx, "host-build", "widgets-3", ptrSessionID("ao-2"), "ao-1"); err != nil {
		t.Fatal(err)
	}
	if got := notes.to("ao-2"); len(got) != 1 || !strings.Contains(got[0], "widgets-3") {
		t.Fatalf("assigned coordinator notices = %q", got)
	}

	notes.ids, notes.messages = nil, nil
	if _, err := svc.AssignOwner(ctx, "host-build", "widgets-3", ptrSessionID("ao-3"), "ao-1"); err != nil {
		t.Fatal(err)
	}
	if got := notes.to("ao-2"); len(got) != 1 || !strings.Contains(got[0], "Ownership removed") {
		t.Fatalf("previous coordinator notices = %q, want ownership-removed", got)
	}
	if got := notes.to("ao-3"); len(got) != 1 || !strings.Contains(got[0], "widgets-3") {
		t.Fatalf("new coordinator notices = %q", got)
	}

	notes.ids, notes.messages = nil, nil
	if _, err := svc.AssignOwner(ctx, "host-build", "widgets-3", nil, "ao-1"); err != nil {
		t.Fatal(err)
	}
	if got := notes.to("ao-3"); len(got) != 1 || !strings.Contains(got[0], "project root") {
		t.Fatalf("coordinator notices after return to root = %q", got)
	}
	if len(notes.ids) != 1 {
		t.Fatalf("notices sent to %v, want only the previous coordinator", notes.ids)
	}
}
