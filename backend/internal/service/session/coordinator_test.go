package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	coordownership "github.com/aoagents/agent-orchestrator/backend/internal/service/ownership"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

// The daemon passes *store.Store as the session store; losing this method
// would silently stop counting remote links toward capacity.
var _ coordownership.LinkLister = (*store.Store)(nil)

// capStore serialises the fake store and lists remote links, so concurrent
// spawn tests stay race-free and link capacity can be counted.
type capStore struct {
	*fakeStore
	mu    *sync.Mutex
	links []domain.RemoteWorkerLink
}

func (s *capStore) GetSession(ctx context.Context, id domain.SessionID) (domain.SessionRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeStore.GetSession(ctx, id)
}

func (s *capStore) ListSessions(ctx context.Context, p domain.ProjectID) ([]domain.SessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeStore.ListSessions(ctx, p)
}

func (s *capStore) GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeStore.GetProject(ctx, id)
}

func (s *capStore) ListPRFactsForSession(ctx context.Context, id domain.SessionID) ([]domain.PRFacts, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeStore.ListPRFactsForSession(ctx, id)
}

func (s *capStore) SetCoordinatorLimits(ctx context.Context, id domain.SessionID, limit *int, slots []domain.HostSlot, event domain.CoordinatorLimitEvent, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeStore.SetCoordinatorLimits(ctx, id, limit, slots, event, at)
}

func (s *capStore) ListRemoteWorkerLinks(context.Context) ([]domain.RemoteWorkerLink, error) {
	return s.links, nil
}

// capFixture is a project with a live root whose spawns insert real rows.
type capFixture struct {
	t     *testing.T
	mu    *sync.Mutex
	store *capStore
	fc    *fakeCommander
	svc   *Service
	next  int
	delay time.Duration
	// inflight and peak measure how many spawns reach the manager at once.
	inflight, peak atomic.Int32
}

// capCommander delays and serialises the fake manager's bookkeeping, not the
// spawns themselves, so overlapping spawns are visible in f.peak.
type capCommander struct {
	*fakeCommander
	f *capFixture
}

func (c *capCommander) Spawn(ctx context.Context, cfg ports.SpawnConfig) (domain.SessionRecord, int, int, error) {
	now := c.f.inflight.Add(1)
	defer c.f.inflight.Add(-1)
	for {
		peak := c.f.peak.Load()
		if now <= peak || c.f.peak.CompareAndSwap(peak, now) {
			break
		}
	}
	time.Sleep(c.f.delay)
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	return c.fakeCommander.Spawn(ctx, cfg)
}

func (c *capCommander) Send(ctx context.Context, id domain.SessionID, message string, att *ports.SpawnAttachment) error {
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	return c.fakeCommander.Send(ctx, id, message, att)
}

func newCapFixture(t *testing.T) *capFixture {
	t.Helper()
	mu := &sync.Mutex{}
	st := newFakeStore()
	st.projects["ao"] = domain.ProjectRecord{ID: "ao", Config: domain.ProjectConfig{
		Coordinators: domain.CoordinatorsConfig{Enabled: true, AllowLocalWorkers: true},
	}}
	f := &capFixture{t: t, mu: mu, store: &capStore{fakeStore: st, mu: mu}}
	f.seed(domain.SessionRecord{ID: "ao-root", ProjectID: "ao", Kind: domain.KindOrchestrator})
	f.fc = &fakeCommander{}
	f.fc.spawnFunc = func(cfg ports.SpawnConfig) domain.SessionRecord {
		f.next++
		rec := domain.SessionRecord{
			ID: domain.SessionID(fmt.Sprintf("ao-n%d", f.next)), ProjectID: cfg.ProjectID, Kind: cfg.Kind,
			OwnerSessionID: cfg.OwnerSessionID, CoordinatorScope: cfg.CoordinatorScope,
			CoordinatorWorkerLimit: cfg.CoordinatorWorkerLimit, CoordinatorHostSlots: cfg.CoordinatorHostSlots,
		}
		st.sessions[rec.ID] = rec
		return rec
	}
	f.svc = &Service{manager: &capCommander{fakeCommander: f.fc, f: f}, store: f.store}
	return f
}

// configure sets the coordinator config; local workers stay allowed so the
// capacity tests can spawn them. configureStrict keeps cfg as given.
func (f *capFixture) configure(cfg domain.CoordinatorsConfig) {
	cfg.AllowLocalWorkers = true
	f.configureStrict(cfg)
}

func (f *capFixture) configureStrict(cfg domain.CoordinatorsConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.store.projects["ao"] = domain.ProjectRecord{ID: "ao", Config: domain.ProjectConfig{Coordinators: cfg}}
}

func (f *capFixture) seed(rec domain.SessionRecord) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.store.sessions[rec.ID] = rec
}

func (f *capFixture) coordinator(id string, terminated bool) domain.SessionID {
	f.seed(domain.SessionRecord{ID: domain.SessionID(id), ProjectID: "ao", Kind: domain.KindCoordinator, CoordinatorScope: id, IsTerminated: terminated})
	return domain.SessionID(id)
}

func (f *capFixture) worker(id string, owner *domain.SessionID, terminated bool) {
	f.seed(domain.SessionRecord{ID: domain.SessionID(id), ProjectID: "ao", Kind: domain.KindWorker, OwnerSessionID: owner, IsTerminated: terminated})
}

func (f *capFixture) spawnCoordinator(parent domain.SessionID, scope string) error {
	_, _, _, err := f.svc.Spawn(context.Background(), ports.SpawnConfig{
		ProjectID: "ao", Kind: domain.KindCoordinator, ParentSessionID: parent, CoordinatorScope: scope,
	})
	return err
}

func (f *capFixture) spawnWorker(parent domain.SessionID) error {
	_, _, _, err := f.svc.Spawn(context.Background(), ports.SpawnConfig{
		ProjectID: "ao", Kind: domain.KindWorker, ParentSessionID: parent,
	})
	return err
}

func wantAPIError(t *testing.T, err error, code string, details map[string]any) {
	t.Helper()
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Code != code {
		t.Fatalf("err = %v, want %s", err, code)
	}
	for key, want := range details {
		if got := apiErr.Details[key]; got != want {
			t.Fatalf("%s details[%s] = %v, want %v", code, key, got, want)
		}
	}
}

func TestSpawnCoordinatorPersistsScopeAndRunsAsyncUnderTheRoot(t *testing.T) {
	f := newCapFixture(t)
	if err := f.spawnCoordinator("ao-root", "  auth\x00 module  "); err != nil {
		t.Fatal(err)
	}
	cfg := f.fc.spawnedCfg
	if cfg.Kind != domain.KindCoordinator || cfg.CoordinatorScope != "auth module" || !cfg.Async {
		t.Fatalf("spawned cfg = %+v", cfg)
	}
	if cfg.OwnerSessionID != nil {
		t.Fatalf("a coordinator has no owner, got %v", *cfg.OwnerSessionID)
	}
	if cfg.Prompt == "" {
		t.Fatal("a coordinator needs a stored prompt so a relaunch resumes")
	}
}

func TestSpawnCoordinatorRequiresTheLiveRoot(t *testing.T) {
	f := newCapFixture(t)
	f.seed(domain.SessionRecord{ID: "ao-old", ProjectID: "ao", Kind: domain.KindOrchestrator, IsTerminated: true})
	f.seed(domain.SessionRecord{ID: "other-root", ProjectID: "other", Kind: domain.KindOrchestrator})
	f.worker("ao-w", nil, false)
	helper := f.coordinator("ao-c", false)

	for name, parent := range map[string]domain.SessionID{
		"no caller": "", "unknown caller": "ao-ghost", "worker": "ao-w", "coordinator": helper,
		"terminated root": "ao-old", "root of another project": "other-root",
	} {
		t.Run(name, func(t *testing.T) {
			before := f.fc.spawnCalls
			err := f.spawnCoordinator(parent, "docs")
			wantAPIError(t, err, "COORDINATOR_ROOT_REQUIRED", map[string]any{"sessionId": string(parent)})
			var apiErr *apierr.Error
			if errors.As(err, &apiErr) && apiErr.Kind != apierr.KindForbidden {
				t.Fatalf("kind = %v, want forbidden", apiErr.Kind)
			}
			if f.fc.spawnCalls != before {
				t.Fatal("a refused spawn must not reach the manager")
			}
		})
	}
}

func TestSpawnCoordinatorValidatesScope(t *testing.T) {
	f := newCapFixture(t)
	if err := f.spawnCoordinator("ao-root", " \t\x01 "); err != nil {
		wantAPIError(t, err, "COORDINATOR_SCOPE_REQUIRED", nil)
	} else {
		t.Fatal("an empty scope must be rejected")
	}
	long := make([]rune, 201)
	for i := range long {
		long[i] = 'é'
	}
	wantAPIError(t, f.spawnCoordinator("ao-root", string(long)), "COORDINATOR_SCOPE_TOO_LONG", map[string]any{"maxLength": 200})
	if f.fc.spawnCalls != 0 {
		t.Fatal("an invalid scope must not reach the manager")
	}
	if err := f.spawnCoordinator("ao-root", string(long[:200])); err != nil {
		t.Fatalf("a 200 character scope is valid: %v", err)
	}
}

func TestSpawnCoordinatorStopsAtTwoActiveCoordinators(t *testing.T) {
	f := newCapFixture(t)
	for i := 0; i < 2; i++ {
		if err := f.spawnCoordinator("ao-root", fmt.Sprintf("scope %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	before := f.fc.spawnCalls
	err := f.spawnCoordinator("ao-root", "third")
	wantAPIError(t, err, "COORDINATOR_LIMIT_REACHED", map[string]any{"projectId": "ao", "current": 2, "allowed": 2})
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) && apiErr.Kind != apierr.KindConflict {
		t.Fatalf("kind = %v, want conflict", apiErr.Kind)
	}
	if f.fc.spawnCalls != before || len(f.store.sessions) != 3 {
		t.Fatalf("a rejected spawn created a session: calls=%d sessions=%d", f.fc.spawnCalls, len(f.store.sessions))
	}
}

func TestTerminatedCoordinatorReleasesItsSlot(t *testing.T) {
	f := newCapFixture(t)
	f.coordinator("ao-c1", true)
	f.coordinator("ao-c2", false)
	f.coordinator("ao-c3", false)
	wantAPIError(t, f.spawnCoordinator("ao-root", "x"), "COORDINATOR_LIMIT_REACHED", nil)

	f.seed(domain.SessionRecord{ID: "ao-c3", ProjectID: "ao", Kind: domain.KindCoordinator, CoordinatorScope: "c3", IsTerminated: true})
	if err := f.spawnCoordinator("ao-root", "x"); err != nil {
		t.Fatalf("terminating a coordinator must release its slot: %v", err)
	}
}

func TestFailedCoordinatorStartupConsumesNoSlot(t *testing.T) {
	f := newCapFixture(t)
	f.fc.spawnErr = errors.New("agent failed to launch")
	for i := 0; i < 3; i++ {
		if err := f.spawnCoordinator("ao-root", "flaky"); err == nil {
			t.Fatal("spawn should fail")
		}
	}
	f.fc.spawnErr = nil
	for i := 0; i < 2; i++ {
		if err := f.spawnCoordinator("ao-root", "ok"); err != nil {
			t.Fatalf("spawn %d after failed startups: %v", i, err)
		}
	}
	wantAPIError(t, f.spawnCoordinator("ao-root", "third"), "COORDINATOR_LIMIT_REACHED", nil)
}

func TestSpawnCoordinatorRefusesWhileRemoteWorkersAreActive(t *testing.T) {
	f := newCapFixture(t)
	f.svc.remoteSpawned = &fakeRemoteSpawned{active: []domain.SessionID{"ao-r1"}}
	wantRemoteWorkersActive(t, f.spawnCoordinator("ao-root", "x"))
	if f.fc.spawnCalls != 0 {
		t.Fatal("the machine-role refusal must stop the spawn")
	}
}

func TestLiveCoordinatorBlocksRemoteWorkersOnThisMachine(t *testing.T) {
	f := newCapFixture(t)
	f.seed(domain.SessionRecord{ID: "ao-root", ProjectID: "ao", Kind: domain.KindOrchestrator, IsTerminated: true})
	f.svc.remoteSpawned = &fakeRemoteSpawned{}
	f.coordinator("ao-c", false)

	_, _, _, err := f.svc.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "ao", Kind: domain.KindWorker, RemoteSpawned: true})
	wantAPIError(t, err, "LOCAL_ORCHESTRATOR_ACTIVE", nil)

	f.coordinator("ao-c", true)
	if _, _, _, err := f.svc.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "ao", Kind: domain.KindWorker, RemoteSpawned: true}); err != nil {
		t.Fatalf("a terminated coordinator no longer blocks remote workers: %v", err)
	}
}

func TestCoordinatorWorkersAreOwnedAndCappedAtThree(t *testing.T) {
	f := newCapFixture(t)
	c := f.coordinator("ao-c1", false)
	for i := 0; i < 3; i++ {
		if err := f.spawnWorker(c); err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
		if got := f.fc.spawnedCfg.OwnerSessionID; got == nil || *got != c {
			t.Fatalf("worker %d owner = %v, want %s", i, got, c)
		}
	}
	before := f.fc.spawnCalls
	err := f.spawnWorker(c)
	wantAPIError(t, err, "COORDINATOR_WORKER_LIMIT_REACHED", map[string]any{"coordinatorId": string(c), "current": 3, "allowed": 3})
	if f.fc.spawnCalls != before {
		t.Fatal("a rejected worker spawn must not reach the manager")
	}
}

func TestCoordinatorWorkerCapIsPerCoordinatorAndReleasedByTermination(t *testing.T) {
	f := newCapFixture(t)
	c1, c2 := f.coordinator("ao-c1", false), f.coordinator("ao-c2", false)
	f.worker("ao-w1", &c1, false)
	f.worker("ao-w2", &c1, false)
	f.worker("ao-w3", &c1, false)

	if err := f.spawnWorker(c2); err != nil {
		t.Fatalf("a full coordinator must not block another one: %v", err)
	}
	wantAPIError(t, f.spawnWorker(c1), "COORDINATOR_WORKER_LIMIT_REACHED", nil)

	f.seed(domain.SessionRecord{ID: "ao-w3", ProjectID: "ao", Kind: domain.KindWorker, OwnerSessionID: &c1, IsTerminated: true})
	if err := f.spawnWorker(c1); err != nil {
		t.Fatalf("a terminated worker must release the owner slot: %v", err)
	}
}

func TestProjectWorkerTotalIncludesRootOwnedAndRemoteWorkers(t *testing.T) {
	f := newCapFixture(t)
	c1, c2 := f.coordinator("ao-c1", false), f.coordinator("ao-c2", false)
	f.worker("ao-w1", &c1, false)
	f.worker("ao-w2", &c1, false)
	f.worker("ao-w3", &c2, false)
	f.worker("ao-w4", nil, false)
	f.store.links = []domain.RemoteWorkerLink{
		{HostID: "h", RemoteSessionID: "r1", ProjectID: "ao", OwnerSessionID: &c2},
		{HostID: "h", RemoteSessionID: "r2", ProjectID: "other"},
	}

	if err := f.spawnWorker("ao-root"); err != nil {
		t.Fatalf("direct root delegation inside the total: %v", err)
	}
	before := f.fc.spawnCalls
	for _, parent := range []domain.SessionID{"ao-root", c1, c2, ""} {
		err := f.spawnWorker(parent)
		wantAPIError(t, err, "PROJECT_WORKER_LIMIT_REACHED", map[string]any{"projectId": "ao", "current": 6, "allowed": 6})
	}
	if f.fc.spawnCalls != before {
		t.Fatal("a rejected worker spawn must not reach the manager")
	}
}

func TestRemoteLinkCountsTowardTheCoordinatorCap(t *testing.T) {
	f := newCapFixture(t)
	c := f.coordinator("ao-c1", false)
	f.worker("ao-w1", &c, false)
	f.store.links = []domain.RemoteWorkerLink{
		{HostID: "h", RemoteSessionID: "r1", ProjectID: "ao", OwnerSessionID: &c},
		{HostID: "h", RemoteSessionID: "r2", ProjectID: "ao", OwnerSessionID: &c},
	}
	wantAPIError(t, f.spawnWorker(c), "COORDINATOR_WORKER_LIMIT_REACHED", map[string]any{"current": 3})
}

func TestProjectWithoutCoordinatorsKeepsUnlimitedWorkers(t *testing.T) {
	f := newCapFixture(t)
	for i := 0; i < 8; i++ {
		if err := f.spawnWorker("ao-root"); err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
}

func TestWorkerSpawnedByAWorkerStaysRootOwnedAndCountsTowardTheTotal(t *testing.T) {
	f := newCapFixture(t)
	c := f.coordinator("ao-c1", false)
	f.worker("ao-w1", &c, false)
	f.worker("ao-w2", &c, false)
	f.worker("ao-w3", &c, false)
	if err := f.spawnWorker("ao-w1"); err != nil {
		t.Fatalf("a worker-spawned worker is root-owned and does not use the coordinator's slots: %v", err)
	}
	if f.fc.spawnedCfg.OwnerSessionID != nil {
		t.Fatalf("owner = %v, want root-owned", *f.fc.spawnedCfg.OwnerSessionID)
	}
}

func TestConcurrentCoordinatorSpawnsNeverExceedTheLimit(t *testing.T) {
	f := newCapFixture(t)
	f.delay = 5 * time.Millisecond
	results := runConcurrently(12, func(int) error { return f.spawnCoordinator("ao-root", "parallel") })
	wantConcurrentOutcome(t, results, 2, "COORDINATOR_LIMIT_REACHED")
	if got := countKind(f, domain.KindCoordinator); got != 2 {
		t.Fatalf("coordinators = %d, want 2", got)
	}
}

func TestConcurrentWorkerSpawnsNeverExceedACoordinatorCap(t *testing.T) {
	f := newCapFixture(t)
	f.delay = 5 * time.Millisecond
	c := f.coordinator("ao-c1", false)
	results := runConcurrently(12, func(int) error { return f.spawnWorker(c) })
	wantConcurrentOutcome(t, results, 3, "COORDINATOR_WORKER_LIMIT_REACHED")
	if got := countKind(f, domain.KindWorker); got != 3 {
		t.Fatalf("workers = %d, want 3", got)
	}
}

func TestConcurrentMixedSpawnsNeverExceedTheProjectTotal(t *testing.T) {
	f := newCapFixture(t)
	f.delay = 5 * time.Millisecond
	c1, c2 := f.coordinator("ao-c1", false), f.coordinator("ao-c2", false)
	parents := []domain.SessionID{c1, c2, "ao-root"}
	results := runConcurrently(30, func(i int) error { return f.spawnWorker(parents[i%len(parents)]) })
	succeeded := 0
	for _, err := range results {
		if err == nil {
			succeeded++
			continue
		}
		var apiErr *apierr.Error
		if !errors.As(err, &apiErr) || (apiErr.Code != "PROJECT_WORKER_LIMIT_REACHED" && apiErr.Code != "COORDINATOR_WORKER_LIMIT_REACHED") {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if succeeded != 6 || countKind(f, domain.KindWorker) != 6 {
		t.Fatalf("succeeded = %d, workers = %d, want 6", succeeded, countKind(f, domain.KindWorker))
	}
	for _, c := range []domain.SessionID{c1, c2} {
		if owned := countOwned(f, c); owned > 3 {
			t.Fatalf("%s owns %d workers", c, owned)
		}
	}
}

func TestConcurrentWorkersWithoutCoordinatorsStayParallel(t *testing.T) {
	f := newCapFixture(t)
	f.delay = 30 * time.Millisecond
	results := runConcurrently(10, func(int) error { return f.spawnWorker("ao-root") })
	for _, err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if peak := f.peak.Load(); peak < 2 {
		t.Fatalf("peak concurrent spawns = %d, ordinary worker spawns were serialised", peak)
	}
}

func TestAssignOwnerRejectsAFullCoordinatorButNotTerminatedOrCurrentWorkers(t *testing.T) {
	f := newCapFixture(t)
	c := f.coordinator("ao-c1", false)
	f.worker("ao-w1", &c, false)
	f.worker("ao-w2", &c, false)
	f.worker("ao-w3", &c, false)
	f.worker("ao-free", nil, false)
	f.worker("ao-done", nil, true)
	ctx := context.Background()

	_, err := f.svc.AssignOwner(ctx, "ao-free", &c, "ao-root")
	wantAPIError(t, err, "COORDINATOR_WORKER_LIMIT_REACHED", map[string]any{"coordinatorId": string(c), "current": 3})
	if got := f.store.sessions["ao-free"].OwnerSessionID; got != nil {
		t.Fatalf("a rejected assignment moved the worker to %v", *got)
	}
	if _, err := f.svc.AssignOwner(ctx, "ao-done", &c, "ao-root"); err != nil {
		t.Fatalf("a terminated worker holds no slot: %v", err)
	}
	if _, err := f.svc.AssignOwner(ctx, "ao-w1", &c, "ao-root"); err != nil {
		t.Fatalf("re-assigning to the current owner is a no-op: %v", err)
	}
	if _, err := f.svc.AssignOwner(ctx, "ao-w1", nil, "ao-root"); err != nil {
		t.Fatalf("moving to the root is never capacity checked: %v", err)
	}
	if _, err := f.svc.AssignOwner(ctx, "ao-free", &c, "ao-root"); err != nil {
		t.Fatalf("the freed slot is usable: %v", err)
	}
}

func TestNonRootCannotMoveWorkersIntoAnotherCoordinator(t *testing.T) {
	f := newCapFixture(t)
	c1, c2 := f.coordinator("ao-c1", false), f.coordinator("ao-c2", false)
	f.worker("ao-w1", &c1, false)
	_, err := f.svc.AssignOwner(context.Background(), "ao-w1", &c2, c1)
	wantAPIError(t, err, "OWNERSHIP_ROOT_ONLY", nil)
}

func TestRestoreRechecksCapacity(t *testing.T) {
	f := newCapFixture(t)
	f.fc.restoreErr = errors.New("restore must not run")
	f.coordinator("ao-c1", false)
	f.coordinator("ao-c2", false)
	f.coordinator("ao-c3", true)
	_, err := f.svc.Restore(context.Background(), "ao-c3")
	wantAPIError(t, err, "COORDINATOR_LIMIT_REACHED", nil)

	c := domain.SessionID("ao-c1")
	f.worker("ao-w1", &c, false)
	f.worker("ao-w2", &c, false)
	f.worker("ao-w3", &c, false)
	f.worker("ao-w4", &c, true)
	_, err = f.svc.Restore(context.Background(), "ao-w4")
	wantAPIError(t, err, "COORDINATOR_WORKER_LIMIT_REACHED", nil)

	f.worker("ao-w5", nil, true)
	f.worker("ao-w6", nil, false)
	f.worker("ao-w7", nil, false)
	f.worker("ao-w8", nil, false)
	_, err = f.svc.Restore(context.Background(), "ao-w5")
	wantAPIError(t, err, "PROJECT_WORKER_LIMIT_REACHED", nil)
}

func TestClaimPRRejectsACoordinator(t *testing.T) {
	f := newCapFixture(t)
	c := f.coordinator("ao-c1", false)
	_, err := f.svc.ClaimPR(context.Background(), c, "1", ClaimPROptions{})
	if !errors.Is(err, ErrSessionNotClaimable) {
		t.Fatalf("err = %v, want ErrSessionNotClaimable", err)
	}
}

func countKind(f *capFixture, kind domain.SessionKind) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, rec := range f.store.sessions {
		if rec.Kind == kind && !rec.IsTerminated {
			n++
		}
	}
	return n
}

func countOwned(f *capFixture, owner domain.SessionID) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, rec := range f.store.sessions {
		if rec.OwnerSessionID != nil && *rec.OwnerSessionID == owner && !rec.IsTerminated {
			n++
		}
	}
	return n
}

func runConcurrently(n int, call func(int) error) []error {
	results := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i] = call(i)
		}()
	}
	close(start)
	wg.Wait()
	return results
}

func wantConcurrentOutcome(t *testing.T, results []error, wantOK int, rejectCode string) {
	t.Helper()
	ok := 0
	for _, err := range results {
		if err == nil {
			ok++
			continue
		}
		var apiErr *apierr.Error
		if !errors.As(err, &apiErr) || apiErr.Code != rejectCode {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != wantOK {
		t.Fatalf("%d spawns succeeded, want %d", ok, wantOK)
	}
}

func TestSpawnCoordinatorIsRefusedWhileCoordinatorsAreDisabled(t *testing.T) {
	f := newCapFixture(t)
	f.configure(domain.CoordinatorsConfig{})
	err := f.spawnCoordinator("ao-root", "docs")
	wantAPIError(t, err, "COORDINATORS_DISABLED", map[string]any{"projectId": "ao"})
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) && apiErr.Kind != apierr.KindConflict {
		t.Fatalf("kind = %v, want conflict", apiErr.Kind)
	}
	if f.fc.spawnCalls != 0 || len(f.store.sessions) != 1 {
		t.Fatalf("a refused spawn created a session: calls=%d sessions=%d", f.fc.spawnCalls, len(f.store.sessions))
	}
}

func TestSpawnCoordinatorHonoursTheConfiguredCoordinatorLimit(t *testing.T) {
	f := newCapFixture(t)
	f.configure(domain.CoordinatorsConfig{Enabled: true, MaxCoordinators: 1})
	if err := f.spawnCoordinator("ao-root", "first"); err != nil {
		t.Fatal(err)
	}
	err := f.spawnCoordinator("ao-root", "second")
	wantAPIError(t, err, "COORDINATOR_LIMIT_REACHED", map[string]any{"projectId": "ao", "current": 1, "allowed": 1})

	f.configure(domain.CoordinatorsConfig{Enabled: true})
	if err := f.spawnCoordinator("ao-root", "second"); err != nil {
		t.Fatalf("a limit back at the default admits the second: %v", err)
	}
	wantAPIError(t, f.spawnCoordinator("ao-root", "third"), "COORDINATOR_LIMIT_REACHED", map[string]any{"allowed": 2})
}

func TestDisabledProjectStillRefusesANonRootCaller(t *testing.T) {
	f := newCapFixture(t)
	f.configure(domain.CoordinatorsConfig{})
	helper := f.coordinator("ao-c", false)
	wantAPIError(t, f.spawnCoordinator(helper, "docs"), "COORDINATOR_ROOT_REQUIRED", nil)
	if f.fc.spawnCalls != 0 {
		t.Fatal("a refused spawn must not reach the manager")
	}
}

func TestDisabledProjectStillRestoresACoordinatorWithinTheLimit(t *testing.T) {
	f := newCapFixture(t)
	f.configure(domain.CoordinatorsConfig{MaxCoordinators: 1})
	f.coordinator("ao-c1", true)
	release, err := f.svc.admitRestore(context.Background(), f.store.sessions["ao-c1"])
	if err != nil {
		t.Fatalf("restore stays allowed for recovery: %v", err)
	}
	release()
	f.coordinator("ao-c2", false)
	_, err = f.svc.admitRestore(context.Background(), f.store.sessions["ao-c1"])
	wantAPIError(t, err, "COORDINATOR_LIMIT_REACHED", map[string]any{"allowed": 1})
}

func TestConfiguredWorkerLimitsReplaceTheFixedNumbers(t *testing.T) {
	f := newCapFixture(t)
	f.configure(domain.CoordinatorsConfig{Enabled: true, MaxWorkersPerCoordinator: 1, MaxWorkers: 2})
	c1 := f.coordinator("ao-c1", false)
	c2 := f.coordinator("ao-c2", false)
	if err := f.spawnWorker(c1); err != nil {
		t.Fatal(err)
	}
	wantAPIError(t, f.spawnWorker(c1), "COORDINATOR_WORKER_LIMIT_REACHED", map[string]any{"coordinatorId": string(c1), "current": 1, "allowed": 1})
	if err := f.spawnWorker(c2); err != nil {
		t.Fatal(err)
	}
	wantAPIError(t, f.spawnWorker("ao-root"), "PROJECT_WORKER_LIMIT_REACHED", map[string]any{"current": 2, "allowed": 2})
}

// spawnBlockedOnProjectLock starts call while this test holds the project
// lock, runs change once call is parked on it, then releases the lock.
func spawnBlockedOnProjectLock(f *capFixture, change func(), call func() error) error {
	lock := f.svc.projectLock("ao")
	lock.Lock()
	done := make(chan error, 1)
	go func() { done <- call() }()
	time.Sleep(50 * time.Millisecond)
	change()
	lock.Unlock()
	return <-done
}

func TestWorkerAdmissionAppliesALimitLoweredWhileItWaitedForTheLock(t *testing.T) {
	f := newCapFixture(t)
	f.configure(domain.CoordinatorsConfig{Enabled: true})
	c := f.coordinator("ao-c1", false)
	f.worker("ao-w1", &c, false)
	f.worker("ao-w2", &c, false)
	err := spawnBlockedOnProjectLock(f,
		func() { f.configure(domain.CoordinatorsConfig{Enabled: true, MaxWorkersPerCoordinator: 2}) },
		func() error { return f.spawnWorker(c) })
	wantAPIError(t, err, "COORDINATOR_WORKER_LIMIT_REACHED", map[string]any{"current": 2, "allowed": 2})
	if f.fc.spawnCalls != 0 {
		t.Fatal("a refused spawn must not reach the manager")
	}
}

func TestCoordinatorSpawnAppliesALimitLoweredWhileItWaitedForTheLock(t *testing.T) {
	f := newCapFixture(t)
	f.coordinator("ao-c1", false)
	err := spawnBlockedOnProjectLock(f,
		func() { f.configure(domain.CoordinatorsConfig{Enabled: true, MaxCoordinators: 1}) },
		func() error { return f.spawnCoordinator("ao-root", "second") })
	wantAPIError(t, err, "COORDINATOR_LIMIT_REACHED", map[string]any{"current": 1, "allowed": 1})
}

func TestCoordinatorSpawnSeesTheGateClosedWhileItWaitedForTheLock(t *testing.T) {
	f := newCapFixture(t)
	err := spawnBlockedOnProjectLock(f,
		func() { f.configure(domain.CoordinatorsConfig{}) },
		func() error { return f.spawnCoordinator("ao-root", "docs") })
	wantAPIError(t, err, "COORDINATORS_DISABLED", map[string]any{"projectId": "ao"})
}

func TestCoordinatorSpawnRechecksTheRootUnderTheLock(t *testing.T) {
	f := newCapFixture(t)
	err := spawnBlockedOnProjectLock(f,
		func() {
			f.seed(domain.SessionRecord{ID: "ao-root", ProjectID: "ao", Kind: domain.KindOrchestrator, IsTerminated: true})
		},
		func() error { return f.spawnCoordinator("ao-root", "docs") })
	wantAPIError(t, err, "COORDINATOR_ROOT_REQUIRED", map[string]any{"sessionId": "ao-root"})
	if f.fc.spawnCalls != 0 {
		t.Fatal("a spawn by a terminated root must not reach the manager")
	}
}
