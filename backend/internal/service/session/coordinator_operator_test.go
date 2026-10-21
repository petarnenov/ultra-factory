package session

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func (f *capFixture) spawnOperatorCoordinator(origin string, limit *int) error {
	_, _, _, err := f.svc.Spawn(context.Background(), ports.SpawnConfig{
		ProjectID: "ao", Kind: domain.KindCoordinator, CoordinatorScope: "docs",
		OperatorInitiated: true, OperatorOrigin: origin, CoordinatorWorkerLimit: limit,
	})
	return err
}

func TestOperatorSpawnStartsACoordinatorUnderTheLiveRootAndAudits(t *testing.T) {
	f := newCapFixture(t)
	limit := 2
	if err := f.spawnOperatorCoordinator("ui", &limit); err != nil {
		t.Fatalf("operator spawn: %v", err)
	}
	if len(f.cfgs) != 1 || f.cfgs[0].ParentSessionID != "ao-root" {
		t.Fatalf("the coordinator must run under the live root, cfg=%+v", f.cfgs)
	}
	events := f.store.operatorEvents
	if len(events) != 1 {
		t.Fatalf("operator events = %d, want 1", len(events))
	}
	ev := events[0]
	if ev.RootSessionID != "ao-root" || ev.Origin != "ui" || ev.Scope != "docs" || ev.ProjectID != "ao" {
		t.Fatalf("audit fields = %+v", ev)
	}
	if ev.WorkerLimit == nil || *ev.WorkerLimit != 2 {
		t.Fatalf("audit must record the worker limit, got %v", ev.WorkerLimit)
	}
	if ev.CoordinatorSessionID == "" {
		t.Fatal("audit must name the new coordinator session")
	}
}

func TestOperatorSpawnNeedsALiveRoot(t *testing.T) {
	f := newCapFixture(t)
	f.seed(domain.SessionRecord{ID: "ao-root", ProjectID: "ao", Kind: domain.KindOrchestrator, IsTerminated: true})

	before := f.fc.spawnCalls
	wantAPIError(t, f.spawnOperatorCoordinator("ui", nil), "COORDINATOR_ROOT_REQUIRED", nil)
	if f.fc.spawnCalls != before || len(f.store.operatorEvents) != 0 {
		t.Fatalf("a refused operator spawn must create nothing: calls=%d events=%d", f.fc.spawnCalls, len(f.store.operatorEvents))
	}
}

func TestOperatorSpawnRefusesACallerAndAnUnknownOrigin(t *testing.T) {
	f := newCapFixture(t)
	_, _, _, err := f.svc.Spawn(context.Background(), ports.SpawnConfig{
		ProjectID: "ao", Kind: domain.KindCoordinator, CoordinatorScope: "docs",
		ParentSessionID: "ao-root", OperatorInitiated: true, OperatorOrigin: "ui",
	})
	wantAPIError(t, err, "OPERATOR_MARKER_NOT_ALLOWED", nil)
	wantAPIError(t, f.spawnOperatorCoordinator("", nil), "COORDINATOR_LIMITS_ORIGIN_INVALID", nil)
	wantAPIError(t, f.spawnOperatorCoordinator("root", nil), "COORDINATOR_LIMITS_ORIGIN_INVALID", nil)
	if f.fc.spawnCalls != 0 || len(f.store.operatorEvents) != 0 {
		t.Fatalf("refused operator spawns created work: calls=%d events=%d", f.fc.spawnCalls, len(f.store.operatorEvents))
	}
}

func TestOperatorMarkerIsOnlyForCoordinators(t *testing.T) {
	f := newCapFixture(t)
	_, _, _, err := f.svc.Spawn(context.Background(), ports.SpawnConfig{
		ProjectID: "ao", Kind: domain.KindWorker, OperatorInitiated: true, OperatorOrigin: "ui",
	})
	wantAPIError(t, err, "OPERATOR_MARKER_NOT_ALLOWED", nil)
}
