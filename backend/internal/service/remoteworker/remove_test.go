package remoteworker

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func removableStore(t *testing.T) *fakeStore {
	t.Helper()
	return &fakeStore{
		sessions: map[domain.SessionID]domain.SessionRecord{
			"ultra-1": {ID: "ultra-1", ProjectID: "ultra-factory", Kind: domain.KindOrchestrator},
			"ultra-2": {ID: "ultra-2", ProjectID: "ultra-factory", Kind: domain.KindWorker},
		},
		stored: []domain.RemoteWorkerLink{{
			HostID: "host-build", RemoteSessionID: "widgets-3", RemoteProjectID: "widgets",
			ProjectID: "ultra-factory", CreatedAt: time.Now().UTC(),
		}},
	}
}

func TestRemoveLinkIsRootOnlyAndRecordsTheRemoval(t *testing.T) {
	store := removableStore(t)
	svc := New(Deps{Store: store})

	if _, err := svc.RemoveLink(context.Background(), "host-build", "widgets-3", "ultra-2"); codeOf(err) != "OWNERSHIP_ROOT_ONLY" {
		t.Fatalf("worker caller: err = %v, want OWNERSHIP_ROOT_ONLY", err)
	}
	if len(store.stored) != 1 || len(store.removals) != 0 {
		t.Fatal("a refused removal changed the store")
	}

	if _, err := svc.RemoveLink(context.Background(), "host-build", "widgets-3", "ultra-1"); err != nil {
		t.Fatal(err)
	}
	if len(store.stored) != 0 || len(store.removals) != 1 || store.removals[0].RemoteSessionID != "widgets-3" {
		t.Fatalf("stored = %d, removals = %+v", len(store.stored), store.removals)
	}
}

func TestRemoveLinkUnknownIsNotFound(t *testing.T) {
	svc := New(Deps{Store: removableStore(t)})
	if _, err := svc.RemoveLink(context.Background(), "host-build", "ghost", "ultra-1"); codeOf(err) != "REMOTE_WORKER_LINK_NOT_FOUND" {
		t.Fatalf("err = %v, want REMOTE_WORKER_LINK_NOT_FOUND", err)
	}
}

func TestReleaseKilledDropsTheLinkWithoutACaller(t *testing.T) {
	store := removableStore(t)
	svc := New(Deps{Store: store})
	for i := 0; i < 2; i++ {
		if err := svc.ReleaseKilled(context.Background(), "host-build", "widgets-3"); err != nil {
			t.Fatalf("release %d: %v", i, err)
		}
	}
	if len(store.stored) != 0 || len(store.removals) != 0 {
		t.Fatalf("stored = %d, removals = %d; want the link gone and no removal", len(store.stored), len(store.removals))
	}
}
