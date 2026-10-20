package session

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

// TestCountCapacityOnTheRealStore pins the census against SQLite: live rows
// and remote links count, terminated rows and other projects do not.
func TestCountCapacityOnTheRealStore(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC().Truncate(time.Second)
	for _, id := range []string{"ao", "other"} {
		if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: id, Path: "/tmp/" + id, RegisteredAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	create := func(project domain.ProjectID, kind domain.SessionKind, scope string, owner *domain.SessionID) domain.SessionRecord {
		t.Helper()
		rec, err := store.CreateSession(ctx, domain.SessionRecord{
			ProjectID: project, Kind: kind, CoordinatorScope: scope, OwnerSessionID: owner, Harness: domain.HarnessClaudeCode,
			Activity:  domain.Activity{State: domain.ActivityActive, LastActivityAt: now},
			CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		return rec
	}
	create("ao", domain.KindOrchestrator, "", nil)
	c1 := create("ao", domain.KindCoordinator, "auth", nil)
	dead := create("ao", domain.KindCoordinator, "old", nil)
	dead.IsTerminated = true
	dead.UpdatedAt = now.Add(time.Second)
	if err := store.UpdateSession(ctx, dead); err != nil {
		t.Fatal(err)
	}
	create("ao", domain.KindWorker, "", &c1.ID)
	create("ao", domain.KindWorker, "", nil)
	create("other", domain.KindWorker, "", nil)
	for _, link := range []domain.RemoteWorkerLink{
		{HostID: "h", RemoteSessionID: "r1", RemoteProjectID: "p", ProjectID: "ao", OwnerSessionID: &c1.ID, CreatedAt: now},
		{HostID: "h", RemoteSessionID: "r2", RemoteProjectID: "p", ProjectID: "other", CreatedAt: now},
	} {
		if err := store.UpsertRemoteWorkerLink(ctx, link); err != nil {
			t.Fatal(err)
		}
	}

	counts, err := (&Service{store: store}).countCapacity(ctx, "ao")
	if err != nil {
		t.Fatal(err)
	}
	if counts.coordinators != 1 || counts.workers != 3 || counts.byOwner[c1.ID] != 2 {
		t.Fatalf("counts = %+v, want 1 coordinator, 3 workers, 2 owned by %s", counts, c1.ID)
	}
}
