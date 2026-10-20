package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func intp(n int) *int { return &n }

func rootCaller(id domain.SessionID) CoordinatorLimitsCaller {
	return CoordinatorLimitsCaller{RequestedBy: id}
}

func opCaller(origin string) CoordinatorLimitsCaller {
	return CoordinatorLimitsCaller{OperatorInitiated: true, Origin: origin}
}

// spawnLimitedCoordinator spawns a coordinator with its own workerLimit and
// returns its id.
func (f *capFixture) spawnLimitedCoordinator(limit *int) (domain.SessionID, error) {
	sess, _, _, err := f.svc.Spawn(context.Background(), ports.SpawnConfig{
		ProjectID: "ao", Kind: domain.KindCoordinator, ParentSessionID: "ao-root",
		CoordinatorScope: "limited", CoordinatorWorkerLimit: limit,
	})
	if err != nil {
		return "", err
	}
	return sess.ID, nil
}

func TestSpawnCoordinatorStoresItsOwnWorkerLimit(t *testing.T) {
	f := newCapFixture(t)
	id, err := f.spawnLimitedCoordinator(intp(2))
	if err != nil {
		t.Fatal(err)
	}
	if got := f.store.sessions[id].CoordinatorWorkerLimit; got == nil || *got != 2 {
		t.Fatalf("stored limit = %v, want 2", got)
	}
	for i := 0; i < 2; i++ {
		if err := f.spawnWorker(id); err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
	err = f.spawnWorker(id)
	wantAPIError(t, err, "COORDINATOR_WORKER_LIMIT_REACHED", map[string]any{"coordinatorId": string(id), "current": 2, "allowed": 2})
}

func TestCoordinatorWithoutWorkerLimitUsesTheProjectValue(t *testing.T) {
	f := newCapFixture(t)
	f.configure(domain.CoordinatorsConfig{Enabled: true, MaxWorkersPerCoordinator: 4, MaxWorkers: 8})
	id, err := f.spawnLimitedCoordinator(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.store.sessions[id].CoordinatorWorkerLimit; got != nil {
		t.Fatalf("stored limit = %d, want none", *got)
	}
	// A coordinator created before the migration has no stored limit either.
	legacy := f.coordinator("ao-legacy", false)
	for _, c := range []domain.SessionID{id, legacy} {
		for i := 0; i < 4; i++ {
			if err := f.spawnWorker(c); err != nil {
				t.Fatalf("%s worker %d: %v", c, i, err)
			}
		}
		wantAPIError(t, f.spawnWorker(c), "COORDINATOR_WORKER_LIMIT_REACHED", map[string]any{"allowed": 4})
		f.configure(domain.CoordinatorsConfig{Enabled: true, MaxWorkersPerCoordinator: 4, MaxWorkers: 12})
	}
}

func TestSpawnCoordinatorRejectsAWorkerLimitOutsideTheProjectValue(t *testing.T) {
	f := newCapFixture(t)
	for _, limit := range []int{0, -1, 4} {
		_, err := f.spawnLimitedCoordinator(intp(limit))
		wantAPIError(t, err, "COORDINATOR_WORKER_LIMIT_INVALID", map[string]any{"value": limit, "max": 3})
	}
	if f.fc.spawnCalls != 0 {
		t.Fatal("an invalid workerLimit must not reach the manager")
	}
	f.configure(domain.CoordinatorsConfig{Enabled: true, MaxWorkersPerCoordinator: 5, MaxWorkers: 10})
	if _, err := f.spawnLimitedCoordinator(intp(5)); err != nil {
		t.Fatalf("a limit equal to the project value is valid: %v", err)
	}
	_, _, _, err := f.svc.Spawn(context.Background(), ports.SpawnConfig{
		ProjectID: "ao", Kind: domain.KindWorker, ParentSessionID: "ao-root", CoordinatorWorkerLimit: intp(2),
	})
	wantAPIError(t, err, "COORDINATOR_WORKER_LIMIT_INVALID", nil)
}

func TestPrecedenceSpawnValueThenProjectDefaultWithinCeilings(t *testing.T) {
	f := newCapFixture(t)
	// A missing project value is the default 3; a spawn value of 2 beats it.
	own, err := f.spawnLimitedCoordinator(intp(2))
	if err != nil {
		t.Fatal(err)
	}
	other := f.coordinator("ao-other", false)
	f.worker("ao-w1", &own, false)
	f.worker("ao-w2", &own, false)
	f.worker("ao-w3", &other, false)
	f.worker("ao-w4", &other, false)
	wantAPIError(t, f.spawnWorker(own), "COORDINATOR_WORKER_LIMIT_REACHED", map[string]any{"allowed": 2})
	if err := f.spawnWorker(other); err != nil {
		t.Fatalf("no spawn value uses the default 3: %v", err)
	}
	// The project ceiling still binds a coordinator whose own limit is above it.
	f.configure(domain.CoordinatorsConfig{Enabled: true, MaxWorkersPerCoordinator: 1, MaxWorkers: 10})
	wantAPIError(t, f.spawnWorker(other), "COORDINATOR_WORKER_LIMIT_REACHED", map[string]any{"allowed": 1})
}

func TestProjectTotalStillBindsWithPerCoordinatorLimits(t *testing.T) {
	f := newCapFixture(t)
	f.configure(domain.CoordinatorsConfig{Enabled: true, MaxWorkersPerCoordinator: 3, MaxWorkers: 3})
	c1, err := f.spawnLimitedCoordinator(intp(2))
	if err != nil {
		t.Fatal(err)
	}
	c2 := f.coordinator("ao-c2", false)
	f.worker("ao-w1", &c1, false)
	f.worker("ao-w2", &c1, false)
	f.worker("ao-r", nil, false)
	wantAPIError(t, f.spawnWorker(c2), "PROJECT_WORKER_LIMIT_REACHED", map[string]any{"allowed": 3})
}

func TestReassignmentToACoordinatorHonoursItsOwnLimit(t *testing.T) {
	f := newCapFixture(t)
	c, err := f.spawnLimitedCoordinator(intp(2))
	if err != nil {
		t.Fatal(err)
	}
	f.worker("ao-w1", &c, false)
	f.worker("ao-w2", &c, false)
	f.worker("ao-free", nil, false)
	_, err = f.svc.AssignOwner(context.Background(), "ao-free", &c, "ao-root")
	wantAPIError(t, err, "COORDINATOR_WORKER_LIMIT_REACHED", map[string]any{"coordinatorId": string(c), "current": 2, "allowed": 2})
}

func TestRestoreKeepsTheStoredLimitAndRechecksTheCoordinatorCap(t *testing.T) {
	f := newCapFixture(t)
	f.seed(domain.SessionRecord{ID: "ao-c1", ProjectID: "ao", Kind: domain.KindCoordinator, CoordinatorScope: "c1", IsTerminated: true, CoordinatorWorkerLimit: intp(1)})
	release, err := f.svc.admitRestore(context.Background(), f.store.sessions["ao-c1"])
	if err != nil {
		t.Fatalf("restore within the cap: %v", err)
	}
	release()
	if got := f.store.sessions["ao-c1"].CoordinatorWorkerLimit; got == nil || *got != 1 {
		t.Fatalf("restore changed the stored limit to %v", got)
	}
	f.seed(domain.SessionRecord{ID: "ao-c1", ProjectID: "ao", Kind: domain.KindCoordinator, CoordinatorScope: "c1", CoordinatorWorkerLimit: intp(1)})
	c := domain.SessionID("ao-c1")
	f.worker("ao-w1", &c, false)
	wantAPIError(t, f.spawnWorker(c), "COORDINATOR_WORKER_LIMIT_REACHED", map[string]any{"allowed": 1})

	f.seed(domain.SessionRecord{ID: "ao-c1", ProjectID: "ao", Kind: domain.KindCoordinator, CoordinatorScope: "c1", IsTerminated: true, CoordinatorWorkerLimit: intp(1)})
	f.coordinator("ao-c2", false)
	f.coordinator("ao-c3", false)
	_, err = f.svc.admitRestore(context.Background(), f.store.sessions["ao-c1"])
	wantAPIError(t, err, "COORDINATOR_LIMIT_REACHED", map[string]any{"allowed": 2})
}

func TestConcurrentSpawnsRaceForTheLastSlotOfALimitedCoordinator(t *testing.T) {
	f := newCapFixture(t)
	f.delay = 5 * time.Millisecond
	c, err := f.spawnLimitedCoordinator(intp(2))
	if err != nil {
		t.Fatal(err)
	}
	f.worker("ao-w1", &c, false)
	results := runConcurrently(8, func(int) error { return f.spawnWorker(c) })
	wantConcurrentOutcome(t, results, 1, "COORDINATOR_WORKER_LIMIT_REACHED")
	if got := countKind(f, domain.KindWorker); got != 2 {
		t.Fatalf("workers = %d, want 2", got)
	}
}

func TestLiveLimitUpdateKeepsWorkersAndBlocksNewOnes(t *testing.T) {
	f := newCapFixture(t)
	c := f.coordinator("ao-c1", false)
	f.worker("ao-w1", &c, false)
	f.worker("ao-w2", &c, false)
	ctx := context.Background()

	if _, err := f.svc.SetCoordinatorWorkerLimit(ctx, c, intp(1), rootCaller("ao-root")); err != nil {
		t.Fatal(err)
	}
	for _, id := range []domain.SessionID{"ao-w1", "ao-w2"} {
		if f.store.sessions[id].IsTerminated {
			t.Fatalf("%s must keep running after the limit is lowered", id)
		}
	}
	wantAPIError(t, f.spawnWorker(c), "COORDINATOR_WORKER_LIMIT_REACHED", map[string]any{"current": 2, "allowed": 1})

	f.seed(domain.SessionRecord{ID: "ao-w1", ProjectID: "ao", Kind: domain.KindWorker, OwnerSessionID: &c, IsTerminated: true})
	wantAPIError(t, f.spawnWorker(c), "COORDINATOR_WORKER_LIMIT_REACHED", map[string]any{"current": 1, "allowed": 1})
	f.seed(domain.SessionRecord{ID: "ao-w2", ProjectID: "ao", Kind: domain.KindWorker, OwnerSessionID: &c, IsTerminated: true})
	if err := f.spawnWorker(c); err != nil {
		t.Fatalf("one worker is admitted after both terminated: %v", err)
	}
	wantAPIError(t, f.spawnWorker(c), "COORDINATOR_WORKER_LIMIT_REACHED", map[string]any{"allowed": 1})
}

func TestLiveLimitUpdateIsAuditedWithTheCallerOrigin(t *testing.T) {
	f := newCapFixture(t)
	c := f.coordinator("ao-c1", false)
	ctx := context.Background()
	if _, err := f.svc.SetCoordinatorWorkerLimit(ctx, c, intp(2), rootCaller("ao-root")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SetCoordinatorWorkerLimit(ctx, c, intp(3), opCaller("ui")); err != nil {
		t.Fatalf("the operator acts on behalf of the live root: %v", err)
	}
	ev := f.store.limitEvents
	if len(ev) != 2 {
		t.Fatalf("audit rows = %d, want 2", len(ev))
	}
	if ev[0].Origin != "api" || ev[0].ActorSessionID != "ao-root" || ev[0].PreviousLimit != nil || ev[0].NewLimit != 2 {
		t.Fatalf("root event = %+v", ev[0])
	}
	if ev[1].Origin != "ui" || ev[1].ActorSessionID != "ao-root" || ev[1].PreviousLimit == nil || *ev[1].PreviousLimit != 2 || ev[1].NewLimit != 3 {
		t.Fatalf("operator event = %+v", ev[1])
	}
}

func TestLiveLimitUpdateRefusesCallersAndInvalidValues(t *testing.T) {
	f := newCapFixture(t)
	c := f.coordinator("ao-c1", false)
	f.worker("ao-w", nil, false)
	f.seed(domain.SessionRecord{ID: "ao-old", ProjectID: "ao", Kind: domain.KindOrchestrator, IsTerminated: true})
	ctx := context.Background()
	for name, caller := range map[string]domain.SessionID{
		"coordinator": c, "worker": "ao-w", "terminated root": "ao-old", "unknown": "ao-ghost", "none": "",
	} {
		_, err := f.svc.SetCoordinatorWorkerLimit(ctx, c, intp(1), rootCaller(caller))
		if err == nil {
			t.Fatalf("%s: a non-root caller must be refused", name)
		}
		wantAPIError(t, err, "COORDINATOR_ROOT_REQUIRED", nil)
	}
	_, err := f.svc.SetCoordinatorWorkerLimit(ctx, c, intp(1), CoordinatorLimitsCaller{RequestedBy: "ao-root", OperatorInitiated: true, Origin: "ui"})
	wantAPIError(t, err, "OPERATOR_MARKER_NOT_ALLOWED", map[string]any{"sessionId": "ao-root"})
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) && apiErr.Kind != apierr.KindForbidden {
		t.Fatalf("kind = %v, want forbidden", apiErr.Kind)
	}
	for _, limit := range []int{0, -1, 4} {
		_, err := f.svc.SetCoordinatorWorkerLimit(ctx, c, intp(limit), rootCaller("ao-root"))
		wantAPIError(t, err, "COORDINATOR_WORKER_LIMIT_INVALID", map[string]any{"max": 3})
	}
	if len(f.store.limitEvents) != 0 || f.store.sessions[c].CoordinatorWorkerLimit != nil {
		t.Fatal("a refused update must change nothing")
	}
}

func TestOperatorLimitUpdateWithoutALiveRootIsRefused(t *testing.T) {
	f := newCapFixture(t)
	c := f.coordinator("ao-c1", false)
	f.seed(domain.SessionRecord{ID: "ao-root", ProjectID: "ao", Kind: domain.KindOrchestrator, IsTerminated: true})
	_, err := f.svc.SetCoordinatorWorkerLimit(context.Background(), c, intp(1), opCaller("ui"))
	wantAPIError(t, err, "COORDINATOR_ROOT_REQUIRED", map[string]any{"projectId": "ao"})
}

func TestLiveLimitUpdateTargetsALiveCoordinatorOnly(t *testing.T) {
	f := newCapFixture(t)
	f.worker("ao-w", nil, false)
	dead := f.coordinator("ao-dead", true)
	ctx := context.Background()
	_, err := f.svc.SetCoordinatorWorkerLimit(ctx, "ao-w", intp(1), rootCaller("ao-root"))
	wantAPIError(t, err, "OWNER_NOT_COORDINATOR", nil)
	_, err = f.svc.SetCoordinatorWorkerLimit(ctx, dead, intp(1), rootCaller("ao-root"))
	wantAPIError(t, err, "OWNER_TERMINATED", nil)
	_, err = f.svc.SetCoordinatorWorkerLimit(ctx, "ao-ghost", intp(1), rootCaller("ao-root"))
	wantAPIError(t, err, "SESSION_NOT_FOUND", nil)
}

func TestLiveLimitUpdateSharesTheSpawnLock(t *testing.T) {
	f := newCapFixture(t)
	c := f.coordinator("ao-c1", false)
	unlock := f.svc.lockOrchestratorProject("ao")
	done := make(chan error, 1)
	go func() {
		_, err := f.svc.SetCoordinatorWorkerLimit(context.Background(), c, intp(1), rootCaller("ao-root"))
		done <- err
	}()
	select {
	case <-done:
		t.Fatal("the update must wait for the project lock like a spawn")
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestOwnershipAndRosterExposeEffectiveLimits(t *testing.T) {
	f := newCapFixture(t)
	f.configure(domain.CoordinatorsConfig{Enabled: true, MaxWorkersPerCoordinator: 4, MaxWorkers: 8})
	c1, err := f.spawnLimitedCoordinator(intp(2))
	if err != nil {
		t.Fatal(err)
	}
	c2 := f.coordinator("ao-c2", false)
	f.worker("ao-w1", &c1, false)
	f.worker("ao-w2", &c2, false)
	f.worker("ao-w3", &c2, false)

	roster, err := f.svc.CoordinatorRoster(context.Background(), "ao", false)
	if err != nil {
		t.Fatal(err)
	}
	if roster.Limits != (domain.CoordinatorLimits{Coordinators: 2, WorkersPerCoordinator: 4, Workers: 8}) {
		t.Fatalf("roster limits = %+v", roster.Limits)
	}
	if roster.Capacity.ActiveCoordinators != 2 || roster.Capacity.ActiveWorkers != 3 {
		t.Fatalf("roster census = %+v", roster.Capacity)
	}
	got := map[domain.SessionID][2]int{}
	for _, e := range roster.Coordinators {
		got[e.Coordinator.ID] = [2]int{e.WorkerLimit, e.ActiveWorkers}
	}
	if got[c1] != [2]int{2, 1} || got[c2] != [2]int{4, 2} {
		t.Fatalf("roster entries = %v", got)
	}
	view, err := f.svc.Ownership(context.Background(), c1)
	if err != nil {
		t.Fatal(err)
	}
	if view.Capacity.WorkerLimit != 2 || view.Capacity.CoordinatorWorkers != 1 || view.Capacity.Limits.Workers != 8 {
		t.Fatalf("ownership capacity = %+v", view.Capacity)
	}
}

func TestLiveLimitUpdateOmittedOrUnchangedIsANoOp(t *testing.T) {
	f := newCapFixture(t)
	c := f.coordinator("ao-c1", false)
	ctx := context.Background()
	if _, err := f.svc.SetCoordinatorWorkerLimit(ctx, c, intp(2), rootCaller("ao-root")); err != nil {
		t.Fatal(err)
	}
	sent, events := len(f.fc.sentMessages), len(f.store.limitEvents)
	for name, limit := range map[string]*int{"omitted": nil, "same as stored": intp(2)} {
		rec, err := f.svc.SetCoordinatorWorkerLimit(ctx, c, limit, rootCaller("ao-root"))
		if err != nil || rec.CoordinatorWorkerLimit == nil || *rec.CoordinatorWorkerLimit != 2 {
			t.Fatalf("%s: rec=%v err=%v, want the stored 2", name, rec.CoordinatorWorkerLimit, err)
		}
	}
	if len(f.fc.sentMessages) != sent || len(f.store.limitEvents) != events {
		t.Fatal("a no-op must write no audit row and send no message")
	}
	// A stored value is compared, not the effective one: setting the project
	// value explicitly on a coordinator with none still writes.
	other := f.coordinator("ao-c2", false)
	if _, err := f.svc.SetCoordinatorWorkerLimit(ctx, other, intp(3), rootCaller("ao-root")); err != nil {
		t.Fatal(err)
	}
	if len(f.store.limitEvents) != events+1 {
		t.Fatal("storing the project value explicitly is a change")
	}
}

func TestLiveLimitUpdateMessagesTheCoordinator(t *testing.T) {
	f := newCapFixture(t)
	c := f.coordinator("ao-c1", false)
	if _, err := f.svc.SetCoordinatorWorkerLimit(context.Background(), c, intp(2), rootCaller("ao-root")); err != nil {
		t.Fatal(err)
	}
	if len(f.fc.sent) != 1 || f.fc.sent[0] != c || f.fc.sentMessages[0] == "" {
		t.Fatalf("sent = %v %q, want one message to %s", f.fc.sent, f.fc.sentMessages, c)
	}
}

func TestLiveLimitUpdateValidatesMarkerAndOrigin(t *testing.T) {
	f := newCapFixture(t)
	c := f.coordinator("ao-c1", false)
	ctx := context.Background()
	_, err := f.svc.SetCoordinatorWorkerLimit(ctx, c, intp(1), CoordinatorLimitsCaller{RequestedBy: "ao-root", OperatorInitiated: true, Origin: "ui"})
	wantAPIError(t, err, "OPERATOR_MARKER_NOT_ALLOWED", map[string]any{"sessionId": "ao-root"})
	for _, caller := range []CoordinatorLimitsCaller{
		{OperatorInitiated: true}, {OperatorInitiated: true, Origin: "web"},
		{RequestedBy: "ao-root", Origin: "ui"},
	} {
		_, err = f.svc.SetCoordinatorWorkerLimit(ctx, c, intp(1), caller)
		wantAPIError(t, err, "COORDINATOR_LIMITS_ORIGIN_INVALID", nil)
	}
	if _, err = f.svc.SetCoordinatorWorkerLimit(ctx, c, intp(1), CoordinatorLimitsCaller{RequestedBy: "ao-root", Origin: "cli"}); err != nil {
		t.Fatal(err)
	}
	if got := f.store.limitEvents[len(f.store.limitEvents)-1].Origin; got != "cli" {
		t.Fatalf("root origin = %q, want cli", got)
	}
}

func TestLoweredProjectCeilingClampsAStoredLimitWithoutRewritingIt(t *testing.T) {
	f := newCapFixture(t)
	f.configure(domain.CoordinatorsConfig{Enabled: true, MaxWorkersPerCoordinator: 5, MaxWorkers: 10})
	c, err := f.spawnLimitedCoordinator(intp(5))
	if err != nil {
		t.Fatal(err)
	}
	f.worker("ao-w1", &c, false)
	f.worker("ao-w2", &c, false)
	f.configure(domain.CoordinatorsConfig{Enabled: true, MaxWorkersPerCoordinator: 2, MaxWorkers: 10})
	wantAPIError(t, f.spawnWorker(c), "COORDINATOR_WORKER_LIMIT_REACHED", map[string]any{"allowed": 2})
	if got := f.store.sessions[c].CoordinatorWorkerLimit; got == nil || *got != 5 {
		t.Fatalf("stored limit = %v, want 5 untouched", got)
	}
	roster, err := f.svc.CoordinatorRoster(context.Background(), "ao", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range roster.Coordinators {
		if e.Coordinator.ID == c && (e.WorkerLimit != 2 || e.Coordinator.CoordinatorWorkerLimit == nil || *e.Coordinator.CoordinatorWorkerLimit != 5) {
			t.Fatalf("roster effective=%d stored=%v, want 2 / 5", e.WorkerLimit, e.Coordinator.CoordinatorWorkerLimit)
		}
	}
	f.configure(domain.CoordinatorsConfig{Enabled: true, MaxWorkersPerCoordinator: 5, MaxWorkers: 10})
	if err := f.spawnWorker(c); err != nil {
		t.Fatalf("raising the ceiling restores the stored 5: %v", err)
	}
}

func TestConcurrentSameValueUpdatesWriteOneAuditRowAndOneMessage(t *testing.T) {
	f := newCapFixture(t)
	c := f.coordinator("ao-c1", false)
	results := runConcurrently(8, func(int) error {
		_, err := f.svc.SetCoordinatorWorkerLimit(context.Background(), c, intp(2), rootCaller("ao-root"))
		return err
	})
	for _, err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.store.limitEvents) != 1 || len(f.fc.sentMessages) != 1 {
		t.Fatalf("audit rows = %d, messages = %d, want 1 and 1", len(f.store.limitEvents), len(f.fc.sentMessages))
	}
}

func TestLimitUpdateSeesATerminationThatLandsBeforeTheLock(t *testing.T) {
	f := newCapFixture(t)
	c := f.coordinator("ao-c1", false)
	unlock := f.svc.lockOrchestratorProject("ao")
	done := make(chan error, 1)
	go func() {
		_, err := f.svc.SetCoordinatorWorkerLimit(context.Background(), c, intp(2), rootCaller("ao-root"))
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	f.seed(domain.SessionRecord{ID: c, ProjectID: "ao", Kind: domain.KindCoordinator, CoordinatorScope: "c", IsTerminated: true})
	unlock()
	wantAPIError(t, <-done, "OWNER_TERMINATED", nil)
	if len(f.store.limitEvents) != 0 {
		t.Fatal("a refused update must write no audit row")
	}
}
