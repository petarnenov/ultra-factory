package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
	coordownership "github.com/aoagents/agent-orchestrator/backend/internal/service/ownership"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/remoteworker"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

// TestConcurrentLocalSpawnsAndRemoteLinksShareTheCoordinatorCap races local
// spawns against link registrations on one store and one shared Guard.
func TestConcurrentLocalSpawnsAndRemoteLinksShareTheCoordinatorCap(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC().Truncate(time.Second)
	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "ao", Path: "/tmp/ao", RegisteredAt: now,
		Config: domain.ProjectConfig{Coordinators: domain.CoordinatorsConfig{AllowLocalWorkers: true}}}); err != nil {
		t.Fatal(err)
	}
	create := func(kind domain.SessionKind, owner *domain.SessionID) domain.SessionRecord {
		rec, err := store.CreateSession(ctx, domain.SessionRecord{
			ProjectID: "ao", Kind: kind, OwnerSessionID: owner, Harness: domain.HarnessClaudeCode,
			CoordinatorScope: map[bool]string{true: "auth"}[kind == domain.KindCoordinator],
			Activity:         domain.Activity{State: domain.ActivityActive, LastActivityAt: now},
			CreatedAt:        now, UpdatedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		return rec
	}
	create(domain.KindOrchestrator, nil)
	c1 := create(domain.KindCoordinator, nil)
	create(domain.KindWorker, &c1.ID)

	guard := &coordownership.Guard{}
	fc := &fakeCommander{spawnFunc: func(cfg ports.SpawnConfig) domain.SessionRecord {
		time.Sleep(5 * time.Millisecond)
		return create(domain.KindWorker, cfg.OwnerSessionID)
	}}
	svc := &Service{manager: &lockedCommander{fakeCommander: fc}, store: store}
	svc.SetCapacityGuard(guard)
	links := remoteworker.New(remoteworker.Deps{
		Store: store, Capacity: guard,
		Lookup: func(string) (remotehost.Entry, bool, error) {
			return remotehost.Entry{Label: "b", URL: "https://b", HostID: "host-build"}, true, nil
		},
	})

	results := runConcurrently(12, func(i int) error {
		if i%2 == 0 {
			_, _, _, err := svc.Spawn(ctx, ports.SpawnConfig{ProjectID: "ao", Kind: domain.KindWorker, ParentSessionID: c1.ID})
			return err
		}
		_, err := links.Register(ctx, remoteworker.RegisterInput{
			ParentSessionID: c1.ID, HostID: "host-build", RemoteSessionID: domain.SessionID(fmt.Sprintf("widgets-%d", i)), RemoteProjectID: "widgets",
		})
		return err
	})
	ok := 0
	for _, err := range results {
		var apiErr *apierr.Error
		switch {
		case err == nil:
			ok++
		case errors.As(err, &apiErr) && apiErr.Code == "COORDINATOR_WORKER_LIMIT_REACHED":
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	counts, err := coordownership.Count(ctx, store, store, "ao")
	if err != nil {
		t.Fatal(err)
	}
	if ok != 2 || counts.ByOwner[c1.ID] != 3 {
		t.Fatalf("succeeded = %d, coordinator owns %d, want 2 and 3", ok, counts.ByOwner[c1.ID])
	}
}

// lockedCommander serialises the fake manager's own bookkeeping.
type lockedCommander struct {
	*fakeCommander
	mu sync.Mutex
}

func (c *lockedCommander) Spawn(ctx context.Context, cfg ports.SpawnConfig) (domain.SessionRecord, int, int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fakeCommander.Spawn(ctx, cfg)
}
