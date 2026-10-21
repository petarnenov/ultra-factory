package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// sentTo returns the messages fc delivered to id, in order.
func sentTo(fc *fakeCommander, id domain.SessionID) []string {
	var out []string
	for i, sent := range fc.sent {
		if sent == id {
			out = append(out, fc.sentMessages[i])
		}
	}
	return out
}

func TestAssignOwnerNoticesTheNewOwnerAndTheWorker(t *testing.T) {
	st, root, coordinator := ownershipStore()
	worker := domain.SessionRecord{ID: "ao-4", ProjectID: "ao", Kind: domain.KindWorker}
	st.sessions[worker.ID] = worker
	fc := &fakeCommander{}
	svc := &Service{store: st, manager: fc}

	if _, err := svc.AssignOwner(context.Background(), worker.ID, &coordinator.ID, root.ID); err != nil {
		t.Fatal(err)
	}
	coord := sentTo(fc, coordinator.ID)
	if len(coord) != 1 || !strings.Contains(coord[0], string(worker.ID)) {
		t.Fatalf("coordinator messages = %q, want one naming %s", coord, worker.ID)
	}
	workerMsgs := sentTo(fc, worker.ID)
	if len(workerMsgs) != 1 || !strings.Contains(workerMsgs[0], string(coordinator.ID)) {
		t.Fatalf("worker messages = %q, want one naming owner %s", workerMsgs, coordinator.ID)
	}
	if len(fc.sent) != 2 {
		t.Fatalf("sent to %v, want only coordinator and worker", fc.sent)
	}
}

func TestReassignNoticesThePreviousLiveCoordinator(t *testing.T) {
	st, root, coordinator := ownershipStore()
	other := domain.SessionRecord{ID: "ao-3", ProjectID: "ao", Kind: domain.KindCoordinator, CoordinatorScope: "docs"}
	st.sessions[other.ID] = other
	worker := domain.SessionRecord{ID: "ao-4", ProjectID: "ao", Kind: domain.KindWorker}
	st.sessions[worker.ID] = worker
	fc := &fakeCommander{}
	svc := &Service{store: st, manager: fc}
	ctx := context.Background()

	if _, err := svc.AssignOwner(ctx, worker.ID, &coordinator.ID, root.ID); err != nil {
		t.Fatal(err)
	}
	fc.sent, fc.sentMessages = nil, nil
	if _, err := svc.AssignOwner(ctx, worker.ID, &other.ID, root.ID); err != nil {
		t.Fatal(err)
	}
	removed := sentTo(fc, coordinator.ID)
	if len(removed) != 1 || !strings.Contains(removed[0], "Ownership removed") ||
		!strings.Contains(removed[0], string(other.ID)) {
		t.Fatalf("previous coordinator messages = %q", removed)
	}
	if got := sentTo(fc, other.ID); len(got) != 1 || !strings.Contains(got[0], string(worker.ID)) {
		t.Fatalf("new coordinator messages = %q", got)
	}
	if got := sentTo(fc, worker.ID); len(got) != 1 || !strings.Contains(got[0], string(other.ID)) {
		t.Fatalf("worker messages = %q, want one naming %s", got, other.ID)
	}
}

func TestReassignBackToRootNoticesTheCoordinatorAndWorker(t *testing.T) {
	st, root, coordinator := ownershipStore()
	worker := domain.SessionRecord{ID: "ao-4", ProjectID: "ao", Kind: domain.KindWorker}
	st.sessions[worker.ID] = worker
	fc := &fakeCommander{}
	svc := &Service{store: st, manager: fc}
	ctx := context.Background()

	if _, err := svc.AssignOwner(ctx, worker.ID, &coordinator.ID, root.ID); err != nil {
		t.Fatal(err)
	}
	fc.sent, fc.sentMessages = nil, nil
	if _, err := svc.AssignOwner(ctx, worker.ID, nil, root.ID); err != nil {
		t.Fatal(err)
	}
	removed := sentTo(fc, coordinator.ID)
	if len(removed) != 1 || !strings.Contains(removed[0], "project root") {
		t.Fatalf("coordinator messages = %q, want ownership-removed to root", removed)
	}
	if got := sentTo(fc, worker.ID); len(got) != 1 || !strings.Contains(got[0], "project root") {
		t.Fatalf("worker messages = %q, want root notice", got)
	}
	if len(fc.sent) != 2 {
		t.Fatalf("sent to %v, want only the coordinator and worker", fc.sent)
	}
}

func TestReassignFromATerminatedCoordinatorSendsNoRemovalNotice(t *testing.T) {
	st, root, coordinator := ownershipStore()
	coordinator.IsTerminated = true
	st.sessions[coordinator.ID] = coordinator
	worker := domain.SessionRecord{ID: "ao-4", ProjectID: "ao", Kind: domain.KindWorker, OwnerSessionID: &coordinator.ID}
	st.sessions[worker.ID] = worker
	fc := &fakeCommander{}
	svc := &Service{store: st, manager: fc}

	if _, err := svc.AssignOwner(context.Background(), worker.ID, nil, root.ID); err != nil {
		t.Fatal(err)
	}
	if got := sentTo(fc, coordinator.ID); len(got) != 0 {
		t.Fatalf("terminated coordinator messages = %q, want none", got)
	}
}

func TestAssignOwnerWakesAHibernatedCoordinatorWithNotice(t *testing.T) {
	st, root, coordinator := ownershipStore()
	asleep := time.Now().UTC()
	coordinator.HibernatedAt = &asleep
	st.sessions[coordinator.ID] = coordinator
	worker := domain.SessionRecord{ID: "ao-4", ProjectID: "ao", Kind: domain.KindWorker}
	st.sessions[worker.ID] = worker
	fc := &fakeCommander{}
	svc := &Service{store: st, manager: fc}

	if _, err := svc.AssignOwner(context.Background(), worker.ID, &coordinator.ID, root.ID); err != nil {
		t.Fatal(err)
	}
	if got := sentTo(fc, coordinator.ID); len(got) != 1 {
		t.Fatalf("hibernated coordinator messages = %q, want one", got)
	}
}

func TestReassignToTheSameOwnerSendsNothing(t *testing.T) {
	st, root, coordinator := ownershipStore()
	worker := domain.SessionRecord{ID: "ao-4", ProjectID: "ao", Kind: domain.KindWorker}
	st.sessions[worker.ID] = worker
	fc := &fakeCommander{}
	svc := &Service{store: st, manager: fc}
	ctx := context.Background()

	if _, err := svc.AssignOwner(ctx, worker.ID, &coordinator.ID, root.ID); err != nil {
		t.Fatal(err)
	}
	fc.sent, fc.sentMessages = nil, nil
	if _, err := svc.AssignOwner(ctx, worker.ID, &coordinator.ID, root.ID); err != nil {
		t.Fatal(err)
	}
	if len(fc.sent) != 0 {
		t.Fatalf("idempotent reassign sent to %v", fc.sent)
	}
}
