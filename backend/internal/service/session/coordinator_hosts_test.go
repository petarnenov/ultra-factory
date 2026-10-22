package session

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	coordownership "github.com/aoagents/agent-orchestrator/backend/internal/service/ownership"
)

type fakeHosts struct {
	paired []coordownership.HostRef
	self   string
}

func (h fakeHosts) PairedHosts(context.Context) ([]coordownership.HostRef, error) {
	return h.paired, nil
}
func (h fakeHosts) SelfHostID() string { return h.self }

// hostFixture is a capacity fixture whose project may use local workers, with
// three paired hosts (Linux and Z600, plus a second host labelled "Dup" twice).
func newHostFixture(t *testing.T, cfg domain.CoordinatorsConfig) *capFixture {
	t.Helper()
	f := newCapFixture(t)
	cfg.Enabled = true
	f.configureStrict(cfg)
	f.svc.SetHostDirectory(fakeHosts{self: "hub", paired: []coordownership.HostRef{
		{ID: "host-linux", Label: "Linux"}, {ID: "host-z600", Label: "Z600"}, {ID: "host-imac", Label: "IMac"},
		{ID: "host-d1", Label: "Dup"}, {ID: "host-d2", Label: "dup"},
	}})
	return f
}

func (f *capFixture) spawnHostCoordinator(limit *int, slots ...domain.HostSlot) (domain.SessionID, error) {
	sess, _, _, err := f.svc.Spawn(context.Background(), ports.SpawnConfig{
		ProjectID: "ao", Kind: domain.KindCoordinator, ParentSessionID: "ao-root",
		CoordinatorScope: "hosts", CoordinatorWorkerLimit: limit, CoordinatorHostSlots: slots,
	})
	return sess.ID, err
}

func TestLocalWorkerOfACoordinatorIsRefusedUnlessTheProjectAllowsIt(t *testing.T) {
	f := newHostFixture(t, domain.CoordinatorsConfig{})
	c := f.coordinator("ao-c1", false)
	before := f.fc.spawnCalls
	wantAPIError(t, f.spawnWorker(c), "COORDINATOR_LOCAL_WORKER_REFUSED", map[string]any{"coordinatorId": string(c), "projectId": "ao"})
	if f.fc.spawnCalls != before || countKind(f, domain.KindWorker) != 0 {
		t.Fatal("a refused local worker must not reach the manager or create a session")
	}
	if err := f.spawnWorker("ao-root"); err != nil {
		t.Fatalf("a root-owned worker is not affected: %v", err)
	}
	f.configureStrict(domain.CoordinatorsConfig{Enabled: true, AllowLocalWorkers: true})
	if err := f.spawnWorker(c); err != nil {
		t.Fatalf("allowLocalWorkers lets the local worker start: %v", err)
	}
	if got := f.store.sessions["ao-n2"]; got.OwnerSessionID == nil || *got.OwnerSessionID != c {
		t.Fatalf("the worker should count toward the coordinator, got owner %v", got.OwnerSessionID)
	}
}

func TestLocalWorkerRuleKeepsTheLimitErrorsFirstAndSkipsRemoteSpawns(t *testing.T) {
	f := newHostFixture(t, domain.CoordinatorsConfig{MaxWorkersPerCoordinator: 1, MaxWorkers: 2})
	c := f.coordinator("ao-c1", false)
	f.worker("ao-w1", &c, false)
	wantAPIError(t, f.spawnWorker(c), "COORDINATOR_WORKER_LIMIT_REACHED", nil)
}

func TestReassigningAndRestoringALocalWorkerFollowTheLocalRule(t *testing.T) {
	f := newHostFixture(t, domain.CoordinatorsConfig{})
	c := f.coordinator("ao-c1", false)
	f.worker("ao-free", nil, false)
	_, err := f.svc.AssignOwner(context.Background(), "ao-free", &c, "ao-root")
	wantAPIError(t, err, "COORDINATOR_LOCAL_WORKER_REFUSED", nil)

	f.worker("ao-dead", &c, true)
	_, err = f.svc.admitRestore(context.Background(), f.store.sessions["ao-dead"])
	wantAPIError(t, err, "COORDINATOR_LOCAL_WORKER_REFUSED", nil)

	f.configureStrict(domain.CoordinatorsConfig{Enabled: true, AllowLocalWorkers: true})
	if _, err := f.svc.AssignOwner(context.Background(), "ao-free", &c, "ao-root"); err != nil {
		t.Fatalf("allowed once the project permits local workers: %v", err)
	}
}

func TestSpawnCoordinatorStoresNormalizedHostSlots(t *testing.T) {
	f := newHostFixture(t, domain.CoordinatorsConfig{MaxWorkersPerCoordinator: 4, MaxWorkers: 8})
	id, err := f.spawnHostCoordinator(intp(4), domain.HostSlot{HostID: "Z600", Slots: 1}, domain.HostSlot{HostID: "host-linux", Slots: 2})
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.HostSlot{{HostID: "host-linux", Slots: 2}, {HostID: "host-z600", Slots: 1}}
	if got := f.store.sessions[id].CoordinatorHostSlots; !coordownership.SameHostSlots(got, want) {
		t.Fatalf("stored slots = %+v, want %+v (IDs, sorted)", got, want)
	}
}

func TestSpawnCoordinatorRejectsInvalidHostSlots(t *testing.T) {
	f := newHostFixture(t, domain.CoordinatorsConfig{MaxWorkersPerCoordinator: 4, MaxWorkers: 8, AllowedHosts: []string{"host-linux", "host-z600", "host-d1", "host-d2"}})
	slot := func(host string, n int) domain.HostSlot { return domain.HostSlot{HostID: host, Slots: n} }
	invalid := map[string]any{"field": "hostSlots"}
	for name, tc := range map[string]struct {
		limit *int
		slots []domain.HostSlot
		code  string
		det   map[string]any
	}{
		"zero slots":          {nil, []domain.HostSlot{slot("Linux", 0)}, "COORDINATOR_WORKER_LIMIT_INVALID", invalid},
		"negative slots":      {nil, []domain.HostSlot{slot("Linux", -1)}, "COORDINATOR_WORKER_LIMIT_INVALID", invalid},
		"same host as ID":     {nil, []domain.HostSlot{slot("Linux", 1), slot("host-linux", 1)}, "COORDINATOR_WORKER_LIMIT_INVALID", invalid},
		"total above limit":   {intp(2), []domain.HostSlot{slot("Linux", 2), slot("Z600", 1)}, "COORDINATOR_WORKER_LIMIT_INVALID", map[string]any{"field": "hostSlots", "max": 2}},
		"total above ceiling": {nil, []domain.HostSlot{slot("Linux", 4), slot("Z600", 1)}, "COORDINATOR_WORKER_LIMIT_INVALID", map[string]any{"max": 4}},
		"outside allowed":     {nil, []domain.HostSlot{slot("IMac", 1)}, "COORDINATOR_HOST_NOT_ALLOWED", map[string]any{"hostId": "host-imac"}},
		"local host":          {nil, []domain.HostSlot{slot("hub", 1)}, "COORDINATOR_HOST_NOT_ALLOWED", map[string]any{"hostId": "hub"}},
		"unknown host":        {nil, []domain.HostSlot{slot("Nowhere", 1)}, "COORDINATOR_HOST_NOT_ALLOWED", map[string]any{"hostId": "Nowhere"}},
		"ambiguous label":     {nil, []domain.HostSlot{slot("Dup", 1)}, "COORDINATOR_HOST_NOT_ALLOWED", map[string]any{"hostId": "Dup"}},
	} {
		before := f.fc.spawnCalls
		_, err := f.spawnHostCoordinator(tc.limit, tc.slots...)
		t.Run(name, func(t *testing.T) { wantAPIError(t, err, tc.code, tc.det) })
		if f.fc.spawnCalls != before {
			t.Fatalf("%s: an invalid slot list must not reach the manager", name)
		}
	}
}

func TestLiveHostSlotUpdateIsPartialAuditedAndValidatedAsAPair(t *testing.T) {
	f := newHostFixture(t, domain.CoordinatorsConfig{MaxWorkersPerCoordinator: 4, MaxWorkers: 8})
	c, err := f.spawnHostCoordinator(intp(3))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	slots := func(s ...domain.HostSlot) CoordinatorLimitsUpdate { return CoordinatorLimitsUpdate{HostSlots: &s} }
	rec, err := f.svc.SetCoordinatorLimits(ctx, c, slots(domain.HostSlot{HostID: "Linux", Slots: 2}), rootCaller("ao-root"))
	if err != nil || len(rec.CoordinatorHostSlots) != 1 || rec.CoordinatorWorkerLimit == nil || *rec.CoordinatorWorkerLimit != 3 {
		t.Fatalf("slots only: rec = %+v err=%v, want the limit kept and one slot", rec, err)
	}
	ev := f.store.limitEvents
	if len(ev) != 1 || len(ev[0].PreviousHostSlots) != 0 || len(ev[0].NewHostSlots) != 1 || ev[0].NewLimit == nil || *ev[0].NewLimit != 3 {
		t.Fatalf("audit = %+v, want one row with the slots before and after", ev)
	}
	// The same pair, written differently, is a no-op: no audit row.
	if _, err := f.svc.SetCoordinatorLimits(ctx, c, slots(domain.HostSlot{HostID: "host-linux", Slots: 2}), opCaller("ui")); err != nil || len(f.store.limitEvents) != 1 {
		t.Fatalf("no-op wrote an audit row: events=%d err=%v", len(f.store.limitEvents), err)
	}
	// Lowering the limit under the kept slots is refused.
	_, err = f.svc.SetCoordinatorLimits(ctx, c, CoordinatorLimitsUpdate{WorkerLimit: intp(1)}, rootCaller("ao-root"))
	wantAPIError(t, err, "COORDINATOR_WORKER_LIMIT_INVALID", map[string]any{"field": "hostSlots", "max": 1})
	// Limit and slots together is one valid pair.
	if _, err := f.svc.SetCoordinatorLimits(ctx, c, CoordinatorLimitsUpdate{WorkerLimit: intp(1), HostSlots: &[]domain.HostSlot{{HostID: "Z600", Slots: 1}}}, rootCaller("ao-root")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SetCoordinatorLimits(ctx, c, slots(), rootCaller("ao-root")); err != nil {
		t.Fatal(err)
	}
	if got := f.store.sessions[c].CoordinatorHostSlots; len(got) != 0 {
		t.Fatalf("an empty list clears the slots, got %+v", got)
	}
	if len(f.store.limitEvents) != 3 {
		t.Fatalf("audit rows = %d, want 3", len(f.store.limitEvents))
	}
}

func TestRosterListsHostSlotsWithUseAndThePlacementLimit(t *testing.T) {
	f := newHostFixture(t, domain.CoordinatorsConfig{MaxWorkersPerCoordinator: 4, MaxWorkers: 8})
	c, err := f.spawnHostCoordinator(intp(4), domain.HostSlot{HostID: "Linux", Slots: 2}, domain.HostSlot{HostID: "Z600", Slots: 1})
	if err != nil {
		t.Fatal(err)
	}
	f.store.links = []domain.RemoteWorkerLink{
		{HostID: "host-linux", RemoteSessionID: "r1", ProjectID: "ao", OwnerSessionID: &c, CreatedAt: time.Now()},
		{HostID: "host-linux", RemoteSessionID: "r2", ProjectID: "ao", OwnerSessionID: &c, CreatedAt: time.Now()},
	}
	roster, err := f.svc.CoordinatorRoster(context.Background(), "ao", false)
	if err != nil {
		t.Fatal(err)
	}
	var entry CoordinatorRosterEntry
	for _, e := range roster.Coordinators {
		if e.Coordinator.ID == c {
			entry = e
		}
	}
	if entry.PlacementLimit != 3 || entry.WorkerLimit != 4 {
		t.Fatalf("limits = placement %d worker %d, want 3 and 4", entry.PlacementLimit, entry.WorkerLimit)
	}
	want := []HostSlotUsage{{HostID: "host-linux", Slots: 2, Used: 2}, {HostID: "host-z600", Slots: 1, Used: 0}}
	if len(entry.HostSlots) != 2 || entry.HostSlots[0] != want[0] || entry.HostSlots[1] != want[1] {
		t.Fatalf("host slots = %+v, want %+v", entry.HostSlots, want)
	}
}

func TestLocalWorkersUseNoHostSlotButCountTowardTheWorkerLimit(t *testing.T) {
	f := newHostFixture(t, domain.CoordinatorsConfig{MaxWorkersPerCoordinator: 4, MaxWorkers: 8, AllowLocalWorkers: true})
	c, err := f.spawnHostCoordinator(intp(3), domain.HostSlot{HostID: "Linux", Slots: 1})
	if err != nil {
		t.Fatal(err)
	}
	// The slot total is 1, yet local workers are outside the slots (ADR 0010
	// decision 3): they fill the worker limit of 3 and no more.
	for i := 0; i < 3; i++ {
		if err := f.spawnWorker(c); err != nil {
			t.Fatalf("local worker %d: %v", i, err)
		}
	}
	wantAPIError(t, f.spawnWorker(c), "COORDINATOR_WORKER_LIMIT_REACHED", map[string]any{"allowed": 3})
}
