// Package coordinatorpilot is the CO-4.1.1 service-level pilot on the real
// store and services; only the session manager and delivery target are fake.
package coordinatorpilot_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/report"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/session"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

const project = domain.ProjectID("ao")

// manager fakes the process-launching manager; the embedded nil *Manager
// covers methods the pilot never calls.
type manager struct {
	*sessionmanager.Manager
	p *pilot
}

// Spawn inserts the session row a real spawn would, from the service's
// resolved config, so capacity and ownership are enforced on real rows.
func (m *manager) Spawn(ctx context.Context, cfg ports.SpawnConfig) (domain.SessionRecord, int, int, error) {
	now := m.p.now()
	rec, err := m.p.store.CreateSession(ctx, domain.SessionRecord{
		ProjectID: cfg.ProjectID, Kind: cfg.Kind, OwnerSessionID: cfg.OwnerSessionID,
		CoordinatorScope: cfg.CoordinatorScope, Harness: domain.HarnessClaudeCode,
		Activity:  domain.Activity{State: domain.ActivityActive, LastActivityAt: now},
		CreatedAt: now, UpdatedAt: now,
	})
	return rec, 0, 0, err
}

// Kill terminates through the durable store write every real termination path
// ends in, so the ownership handback trigger runs.
func (m *manager) Kill(ctx context.Context, id domain.SessionID) (bool, error) {
	rec, ok, err := m.p.store.GetSession(ctx, id)
	if err != nil || !ok {
		return false, err
	}
	rec.IsTerminated = true
	rec.UpdatedAt = m.p.now()
	return true, m.p.store.UpdateSession(ctx, rec)
}

// Send accepts ownership notices; the pilot checks report delivery through
// its own delivery fake, so the notices need no recipient here.
func (m *manager) Send(context.Context, domain.SessionID, string, *ports.SpawnAttachment) error {
	return nil
}

// The service probes these optional capabilities on its manager; the embedded
// nil Manager would panic on them.
func (m *manager) StatusRecoveryRevision() uint64 { return 0 }

func (m *manager) SessionStatusReadiness(domain.SessionRecord) string { return "" }

type submission struct {
	target, message, key string
}

// delivery is the semantic target; it records attempts and accepted turns
// separately because only an accepted turn is read.
type delivery struct {
	mu         sync.Mutex
	fail       error
	attempts   []submission
	accepted   []submission
	interrupts []domain.SessionID
}

func (d *delivery) Submit(_ context.Context, target domain.SessionID, message, key string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.attempts = append(d.attempts, submission{string(target), message, key})
	if d.fail != nil {
		return d.fail
	}
	d.accepted = append(d.accepted, submission{string(target), message, key})
	return nil
}

func (d *delivery) Interrupt(_ context.Context, target domain.SessionID) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.interrupts = append(d.interrupts, target)
	return nil
}

func (d *delivery) setFail(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.fail = err
}

// receivedBy counts accepted turns for target whose text contains needle.
func (d *delivery) receivedBy(target domain.SessionID, needle string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, s := range d.accepted {
		if s.target == string(target) && strings.Contains(s.message, needle) {
			n++
		}
	}
	return n
}

// anyReceived counts accepted turns for any target containing needle.
func (d *delivery) anyReceived(needle string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, s := range d.accepted {
		if strings.Contains(s.message, needle) {
			n++
		}
	}
	return n
}

type pilot struct {
	t      *testing.T
	dir    string
	store  *sqlite.Store
	clock  atomic.Int64
	svc    *session.Service
	deliv  *delivery
	rep    *report.Service
	sum    *report.SummaryService
	coord  *report.Coordinator
	root   domain.SessionRecord
	nextID atomic.Int64
}

func (p *pilot) now() time.Time { return time.Unix(0, p.clock.Load()).UTC() }

func (p *pilot) advance(d time.Duration) { p.clock.Add(int64(d)) }

// wire builds every service over p.store. A daemon restart rebuilds exactly
// this set over the same database.
func (p *pilot) wire() {
	p.svc = session.NewWithDeps(session.Deps{Manager: &manager{p: p}, Store: p.store, Clock: p.now})
	p.rep = report.New(report.Deps{Store: p.store, Now: p.now, OnCreated: func(domain.ReportRecord) { p.coord.Wake() }, NewID: func() string { return fmt.Sprintf("rpt-%03d", p.nextID.Add(1)) }})
	p.sum = report.NewSummaryService(report.SummaryDeps{Store: p.store, Now: p.now, NewID: func() string { return fmt.Sprintf("csum-%03d", p.nextID.Add(1)) }})
	p.coord = report.NewCoordinator(report.CoordinatorDeps{
		Store: p.store, Delivery: p.deliv, Summaries: p.store, Now: p.now, RetryDelay: 20 * time.Millisecond,
		NewToken: func() string { return fmt.Sprintf("claim-%03d", p.nextID.Add(1)) },
	})
}

func newPilot(t *testing.T) *pilot {
	t.Helper()
	p := &pilot{t: t, dir: t.TempDir(), deliv: &delivery{}}
	p.clock.Store(time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC).UnixNano())
	store, err := sqlitetest.Open(p.dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	p.store = store
	t.Cleanup(func() { _ = p.store.Close() })
	ctx := context.Background()
	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: string(project), Path: "/tmp/ao-pilot", RegisteredAt: p.now(),
		Config: domain.ProjectConfig{Coordinators: domain.CoordinatorsConfig{Enabled: true, AllowLocalWorkers: true}}}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	p.root, err = store.CreateSession(ctx, domain.SessionRecord{
		ProjectID: project, Kind: domain.KindOrchestrator, Harness: domain.HarnessClaudeCode,
		Activity:  domain.Activity{State: domain.ActivityActive, LastActivityAt: p.now()},
		CreatedAt: p.now(), UpdatedAt: p.now(),
	})
	if err != nil {
		t.Fatalf("seed root: %v", err)
	}
	p.wire()
	return p
}

// restart reopens the database, requeues claims as daemon.go does, and
// rebuilds every service.
func (p *pilot) restart() (requeuedReports, requeuedSummaries int64) {
	p.t.Helper()
	ctx := context.Background()
	if err := p.store.Close(); err != nil {
		p.t.Fatalf("close store: %v", err)
	}
	store, err := sqlite.OpenPreMigrated(p.dir)
	if err != nil {
		p.t.Fatalf("reopen store: %v", err)
	}
	p.store = store
	if requeuedReports, err = store.RequeueClaimedReports(ctx); err != nil {
		p.t.Fatalf("requeue reports: %v", err)
	}
	if requeuedSummaries, err = store.RequeueClaimedCoordinatorSummaries(ctx); err != nil {
		p.t.Fatalf("requeue summaries: %v", err)
	}
	p.wire()
	return requeuedReports, requeuedSummaries
}

func (p *pilot) spawn(kind domain.SessionKind, parent domain.SessionID, scope string) (domain.Session, error) {
	p.t.Helper()
	s, _, _, err := p.svc.Spawn(context.Background(), ports.SpawnConfig{
		ProjectID: project, Kind: kind, ParentSessionID: parent, CoordinatorScope: scope,
	})
	return s, err
}

func (p *pilot) mustSpawn(kind domain.SessionKind, parent domain.SessionID, scope string) domain.SessionID {
	p.t.Helper()
	s, err := p.spawn(kind, parent, scope)
	if err != nil {
		p.t.Fatalf("spawn %s under %q: %v", kind, parent, err)
	}
	return s.ID
}

func (p *pilot) get(id domain.SessionID) domain.SessionRecord {
	p.t.Helper()
	rec, ok, err := p.store.GetSession(context.Background(), id)
	if err != nil || !ok {
		p.t.Fatalf("get %s: ok=%v err=%v", id, ok, err)
	}
	return rec
}

func (p *pilot) ownerOf(id domain.SessionID) string {
	if o := p.get(id).OwnerSessionID; o != nil {
		return string(*o)
	}
	return ""
}

// reportFrom files a needs_input report, which is due at once, with a note
// unique to the worker so deliveries can be attributed.
func (p *pilot) reportFrom(worker domain.SessionID, tag string) string {
	p.t.Helper()
	note := fmt.Sprintf("NOTE-%s-%s", worker, tag)
	if _, err := p.rep.Create(context.Background(), report.CreateInput{SessionID: worker, State: domain.ReportNeedsInput, Note: note}); err != nil {
		p.t.Fatalf("report from %s: %v", worker, err)
	}
	return note
}

func (p *pilot) runDue() error {
	_, err := p.coord.RunDue(context.Background())
	return err
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Code != code {
		t.Fatalf("error = %v, want API code %s", err, code)
	}
}

// TestCoordinatorHierarchyEndToEnd walks ADR 0010 sections 3 to 9 in order.
// The steps share one world on purpose: each leaves the state the next needs.
func TestCoordinatorHierarchyEndToEnd(t *testing.T) {
	p := newPilot(t)
	ctx := context.Background()
	root := p.root.ID
	var coordA, coordB domain.SessionID
	var workersA, workersB []domain.SessionID
	const hostID, remoteID = "host-build", domain.SessionID("widgets-7")

	t.Run("one root and two coordinators with distinct scopes", func(t *testing.T) {
		coordA = p.mustSpawn(domain.KindCoordinator, root, "auth area")
		coordB = p.mustSpawn(domain.KindCoordinator, root, "billing area")
		if a, b := p.get(coordA), p.get(coordB); a.CoordinatorScope == b.CoordinatorScope || a.Kind != domain.KindCoordinator || b.Kind != domain.KindCoordinator {
			t.Fatalf("coordinators = %+v / %+v", a, b)
		}
		_, err := p.spawn(domain.KindCoordinator, root, "third area")
		wantCode(t, err, "COORDINATOR_LIMIT_REACHED")
		_, err = p.spawn(domain.KindCoordinator, coordA, "self-replicated area")
		wantCode(t, err, "COORDINATOR_ROOT_REQUIRED")
		if _, err = p.spawn(domain.KindCoordinator, root, "  "); err == nil {
			t.Fatal("an empty scope was accepted")
		}
	})

	t.Run("three workers per coordinator, the fourth is rejected under concurrency", func(t *testing.T) {
		// Six concurrent spawns race for coordinator A's three slots.
		var wg sync.WaitGroup
		var okIDs sync.Map
		var rejected atomic.Int32
		for i := 0; i < 6; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s, err := p.spawn(domain.KindWorker, coordA, "")
				if err == nil {
					okIDs.Store(s.ID, true)
					return
				}
				wantCode(t, err, "COORDINATOR_WORKER_LIMIT_REACHED")
				rejected.Add(1)
			}()
		}
		wg.Wait()
		okIDs.Range(func(k, _ any) bool { workersA = append(workersA, k.(domain.SessionID)); return true })
		if len(workersA) != 3 || rejected.Load() != 3 {
			t.Fatalf("coordinator A admitted %d and rejected %d, want 3 and 3", len(workersA), rejected.Load())
		}
		for _, w := range workersA {
			if p.ownerOf(w) != string(coordA) {
				t.Fatalf("worker %s owner = %q, want %s", w, p.ownerOf(w), coordA)
			}
		}

		// Coordinator B: two local workers plus one remote link fill its three
		// slots, so a remote link is counted exactly like a local worker.
		workersB = append(workersB, p.mustSpawn(domain.KindWorker, coordB, ""), p.mustSpawn(domain.KindWorker, coordB, ""))
		link := domain.RemoteWorkerLink{HostID: hostID, HostLabel: "build", RemoteSessionID: remoteID, RemoteProjectID: "widgets", ProjectID: project, DisplayName: "remote seven", CreatedAt: p.now()}
		if err := p.store.UpsertRemoteWorkerLink(ctx, link); err != nil {
			t.Fatalf("seed remote link: %v", err)
		}
		if _, ok, err := p.store.SetRemoteWorkerOwner(ctx, hostID, remoteID, &coordB, domain.OwnershipExplicitAssignment, p.now()); err != nil || !ok {
			t.Fatalf("assign remote link to B: ok=%v err=%v", ok, err)
		}
		_, err := p.spawn(domain.KindWorker, coordB, "")
		wantCode(t, err, "COORDINATOR_WORKER_LIMIT_REACHED")

		// The project is now at six: a root-owned direct worker is the seventh.
		_, err = p.spawn(domain.KindWorker, root, "")
		wantCode(t, err, "PROJECT_WORKER_LIMIT_REACHED")
	})

	t.Run("each report reaches its assigned coordinator exactly once", func(t *testing.T) {
		notes := map[domain.SessionID]string{}
		for _, w := range append(append([]domain.SessionID{}, workersA...), workersB...) {
			notes[w] = p.reportFrom(w, "first")
		}
		if err := p.runDue(); err != nil {
			t.Fatalf("delivery pass: %v", err)
		}
		if err := p.runDue(); err != nil {
			t.Fatalf("second delivery pass: %v", err)
		}
		for w, note := range notes {
			owner := domain.SessionID(p.ownerOf(w))
			if got := p.deliv.receivedBy(owner, note); got != 1 {
				t.Errorf("worker %s: owner %s received its report %d times, want 1", w, owner, got)
			}
			if got := p.deliv.anyReceived(note); got != 1 {
				t.Errorf("worker %s: report reached %d recipients in total, want 1", w, got)
			}
			if got := p.deliv.receivedBy(root, note); got != 0 {
				t.Errorf("worker %s: the root also received a coordinator-owned report", w)
			}
		}
		for _, r := range mustReports(t, p) {
			if r.DeliveryState != domain.ReportAcknowledged {
				t.Errorf("report %s state = %s, want acknowledged", r.ID, r.DeliveryState)
			}
		}
	})

	t.Run("a remote worker's report reaches the coordinator that owns its link", func(t *testing.T) {
		links, err := p.store.ListRemoteWorkerLinks(ctx)
		if err != nil || len(links) != 1 {
			t.Fatalf("links = %v err=%v", links, err)
		}
		recipient, ok, err := p.coord.ResolveRemoteRecipient(ctx, links[0])
		if err != nil || !ok || recipient.ID != coordB {
			t.Fatalf("remote recipient = %s ok=%v err=%v, want %s", recipient.ID, ok, err, coordB)
		}
		remote := domain.ReportRecord{ID: "remote-rpt-1", SessionID: remoteID, ProjectID: project, State: domain.ReportNeedsInput, Note: "NOTE-remote-first", CreatedAt: p.now(), DeliveryState: domain.ReportPending, AvailableAt: p.now(), RepeatCount: 1}
		if err := p.coord.DeliverExternal(ctx, project, recipient.ID, []domain.ReportRecord{remote}); err != nil {
			t.Fatalf("deliver external: %v", err)
		}
		if got := p.deliv.receivedBy(coordB, "NOTE-remote-first"); got != 1 {
			t.Fatalf("coordinator B received the remote report %d times, want 1", got)
		}
		if got := p.deliv.receivedBy(root, "NOTE-remote-first"); got != 0 {
			t.Fatal("the root received a coordinator-owned remote report")
		}
	})

	t.Run("coordinator summaries and an escalation reach the root", func(t *testing.T) {
		in := report.SummaryInput{
			CoordinatorSessionID: coordA, SubmissionKey: "a-1", State: domain.SummaryWorking,
			WorkerSessionIDs: workersA, Result: "SUMMARY-A-working",
		}
		first, created, err := p.sum.Submit(ctx, in)
		if err != nil || !created {
			t.Fatalf("submit summary: created=%v err=%v", created, err)
		}
		again, created, err := p.sum.Submit(ctx, in)
		if err != nil || created || again.ID != first.ID {
			t.Fatalf("duplicate submission: created=%v id=%s err=%v, want the original %s", created, again.ID, err, first.ID)
		}
		if _, _, err := p.sum.Submit(ctx, report.SummaryInput{
			CoordinatorSessionID: coordB, SubmissionKey: "b-esc", State: domain.SummaryBlocked,
			EscalationReason: domain.EscalationScopeDecision, Result: "SUMMARY-B-escalation",
		}); err != nil {
			t.Fatalf("submit escalation: %v", err)
		}
		// A summary is rejected for a worker's session and for the root.
		if _, _, err := p.sum.Submit(ctx, report.SummaryInput{CoordinatorSessionID: workersA[0], SubmissionKey: "x", State: domain.SummaryWorking}); err == nil {
			t.Fatal("a worker submitted a coordinator summary")
		}
		p.advance(time.Minute)
		if err := p.runDue(); err != nil {
			t.Fatalf("summary delivery pass: %v", err)
		}
		if err := p.runDue(); err != nil {
			t.Fatalf("second summary delivery pass: %v", err)
		}
		for _, needle := range []string{"SUMMARY-A-working", "SUMMARY-B-escalation"} {
			if got := p.deliv.receivedBy(root, needle); got != 1 {
				t.Errorf("root received %q %d times, want 1", needle, got)
			}
			if got := p.deliv.anyReceived(needle); got != 1 {
				t.Errorf("%q reached %d recipients, want 1 (the root only)", needle, got)
			}
		}
		p.deliv.mu.Lock()
		interrupted := len(p.deliv.interrupts)
		p.deliv.mu.Unlock()
		if interrupted < 1 {
			t.Error("an escalation did not interrupt the root")
		}
	})

	t.Run("a delivery failure retries once the target recovers, without loss or duplicate", func(t *testing.T) {
		p.deliv.setFail(errors.New("target unavailable"))
		note := p.reportFrom(workersA[0], "while-down")
		before := len(p.deliv.attempts)
		for i := 0; i < 2; i++ {
			_ = p.runDue()
			p.advance(10 * time.Minute)
		}
		if p.deliv.anyReceived(note) != 0 {
			t.Fatal("a failed delivery was recorded as accepted")
		}
		if len(p.deliv.attempts) <= before {
			t.Fatal("no delivery was attempted while the target was down")
		}
		if r := reportByNote(t, p, note); r.DeliveryState != domain.ReportPending {
			t.Fatalf("report state after failed delivery = %s, want pending", r.DeliveryState)
		}
		p.deliv.setFail(nil)
		if err := p.runDue(); err != nil {
			t.Fatalf("delivery after recovery: %v", err)
		}
		_ = p.runDue()
		if got := p.deliv.receivedBy(coordA, note); got != 1 {
			t.Fatalf("coordinator A received the retried report %d times, want 1", got)
		}
		// Every attempt of one batch carried the same idempotency key.
		keys := map[string]bool{}
		for _, s := range p.deliv.attempts {
			if strings.Contains(s.message, note) {
				keys[s.key] = true
			}
		}
		if len(keys) != 1 {
			t.Fatalf("retried batch used %d idempotency keys, want 1: %v", len(keys), keys)
		}
	})

	t.Run("a daemon restart during pending delivery loses and duplicates nothing", func(t *testing.T) {
		report1 := p.reportFrom(workersB[0], "crash-report")
		if _, _, err := p.sum.Submit(ctx, report.SummaryInput{
			CoordinatorSessionID: coordB, SubmissionKey: "b-crash", State: domain.SummaryDone,
			WorkerSessionIDs: workersB, Result: "SUMMARY-B-crash",
		}); err != nil {
			t.Fatalf("submit summary: %v", err)
		}
		// The daemon dies after claiming both batches and before any submit.
		batch, err := p.store.ClaimPendingReportBatch(ctx, project, coordB, root, "dead-daemon-reports", p.now())
		if err != nil || len(batch) != 1 {
			t.Fatalf("claim report batch before the crash: %d rows, err=%v", len(batch), err)
		}
		p.advance(time.Minute)
		sbatch, err := p.store.ClaimPendingCoordinatorSummaryBatch(ctx, project, coordB, root, "dead-daemon-summary", p.now())
		if err != nil || len(sbatch) != 1 {
			t.Fatalf("claim summary batch before the crash: %d rows, err=%v", len(sbatch), err)
		}
		if p.deliv.anyReceived(report1) != 0 || p.deliv.anyReceived("SUMMARY-B-crash") != 0 {
			t.Fatal("something was delivered before the restart")
		}

		requeuedReports, requeuedSummaries := p.restart()
		if requeuedReports != 1 || requeuedSummaries != 1 {
			t.Fatalf("restart requeued %d reports and %d summaries, want 1 and 1", requeuedReports, requeuedSummaries)
		}
		p.advance(time.Minute)
		for i := 0; i < 2; i++ {
			if err := p.runDue(); err != nil {
				t.Fatalf("delivery pass %d after restart: %v", i, err)
			}
		}
		if got := p.deliv.receivedBy(coordB, report1); got != 1 {
			t.Errorf("coordinator B received the crashed report %d times, want 1", got)
		}
		if got := p.deliv.receivedBy(root, "SUMMARY-B-crash"); got != 1 {
			t.Errorf("root received the crashed summary %d times, want 1", got)
		}
		// A second restart with nothing pending changes nothing.
		if rr, rs := p.restart(); rr != 0 || rs != 0 {
			t.Fatalf("idle restart requeued %d and %d rows", rr, rs)
		}
		_ = p.runDue()
		if got := p.deliv.receivedBy(coordB, report1); got != 1 {
			t.Errorf("after an idle restart coordinator B has the report %d times, want 1", got)
		}
	})

	t.Run("terminating a coordinator hands its workers to the root and tells the root", func(t *testing.T) {
		pendingNote := p.reportFrom(workersA[1], "pending-at-termination")
		if _, err := p.svc.Kill(ctx, coordA); err != nil {
			t.Fatalf("kill coordinator: %v", err)
		}
		for _, w := range workersA {
			if p.ownerOf(w) != "" {
				t.Errorf("worker %s still owned by %q after its coordinator terminated", w, p.ownerOf(w))
			}
			if p.get(w).IsTerminated {
				t.Errorf("worker %s was terminated with its coordinator", w)
			}
		}
		p.advance(time.Minute)
		_ = p.runDue()
		_ = p.runDue()
		if got := p.deliv.receivedBy(root, pendingNote); got != 1 {
			t.Errorf("root received the orphaned pending report %d times, want 1", got)
		}
		if got := p.deliv.receivedBy(coordA, pendingNote); got != 0 {
			t.Errorf("the terminated coordinator received a report")
		}
	})

	t.Run("recovery: a replacement coordinator takes the workers back through the supported path", func(t *testing.T) {
		// Coordinator A's slot is free again, so a replacement can be spawned.
		coordC := p.mustSpawn(domain.KindCoordinator, root, "auth area, replacement")
		_, err := p.spawn(domain.KindCoordinator, root, "fourth area")
		wantCode(t, err, "COORDINATOR_LIMIT_REACHED")
		// Only the root writes ownership.
		_, err = p.svc.AssignOwner(ctx, workersA[0], &coordC, coordB)
		wantCode(t, err, "OWNERSHIP_ROOT_ONLY")
		_, err = p.svc.AssignOwner(ctx, workersA[0], &coordC, workersA[1])
		wantCode(t, err, "OWNERSHIP_ROOT_ONLY")
		// The dead coordinator cannot be an owner again.
		_, err = p.svc.AssignOwner(ctx, workersA[0], &coordA, root)
		wantCode(t, err, "OWNER_TERMINATED")
		for _, w := range workersA {
			if _, err := p.svc.AssignOwner(ctx, w, &coordC, root); err != nil {
				t.Fatalf("reassign %s to %s: %v", w, coordC, err)
			}
		}
		// Reassigning the same owner again is a no-op, not an error.
		if _, err := p.svc.AssignOwner(ctx, workersA[0], &coordC, root); err != nil {
			t.Fatalf("idempotent reassignment: %v", err)
		}
		// C is full; B is full too, so a fourth worker cannot be moved in.
		extra := p.seedRootWorker(t)
		_, err = p.svc.AssignOwner(ctx, extra, &coordC, root)
		wantCode(t, err, "COORDINATOR_WORKER_LIMIT_REACHED")

		note := p.reportFrom(workersA[2], "after-recovery")
		_ = p.runDue()
		if got := p.deliv.receivedBy(coordC, note); got != 1 {
			t.Fatalf("replacement coordinator received the report %d times, want 1", got)
		}
		if got := p.deliv.receivedBy(root, note); got != 0 {
			t.Fatal("the root received a report of a worker it handed to the replacement")
		}
	})
}

// seedRootWorker inserts a root-owned worker directly, the way a grandfathered
// row exists, so the reassignment cap is exercised without spawn admission.
func (p *pilot) seedRootWorker(t *testing.T) domain.SessionID {
	t.Helper()
	rec, err := p.store.CreateSession(context.Background(), domain.SessionRecord{
		ProjectID: project, Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode,
		Activity:  domain.Activity{State: domain.ActivityActive, LastActivityAt: p.now()},
		CreatedAt: p.now(), UpdatedAt: p.now(),
	})
	if err != nil {
		t.Fatalf("seed root-owned worker: %v", err)
	}
	return rec.ID
}

// TestCoordinatorTerminationEscalatesToRoot holds ADR 0010 sections 6 and 7:
// termination escalates to the root, listing the returned workers.
func TestCoordinatorTerminationEscalatesToRoot(t *testing.T) {
	p := newPilot(t)
	ctx := context.Background()
	coord := p.mustSpawn(domain.KindCoordinator, p.root.ID, "auth area")
	worker := p.mustSpawn(domain.KindWorker, coord, "")
	if _, err := p.svc.Kill(ctx, coord); err != nil {
		t.Fatalf("kill coordinator: %v", err)
	}
	p.advance(time.Minute)
	for i := 0; i < 2; i++ {
		if err := p.runDue(); err != nil {
			t.Fatalf("delivery pass: %v", err)
		}
	}
	if got := p.deliv.receivedBy(p.root.ID, "coordinator_terminated"); got != 1 {
		t.Fatalf("root received %d coordinator_terminated escalations, want 1", got)
	}
	if got := p.deliv.receivedBy(p.root.ID, string(worker)); got != 1 {
		t.Fatalf("the escalation does not list the returned worker %s", worker)
	}
}

// TestCoordinatorTerminationEscalationDelivery covers the escalation's
// delivery: immediate, retried, written once, and sent with no workers.
func TestCoordinatorTerminationEscalationDelivery(t *testing.T) {
	t.Run("no owned workers still escalates once", func(t *testing.T) {
		p := newPilot(t)
		coord := p.mustSpawn(domain.KindCoordinator, p.root.ID, "docs area")
		if _, err := p.svc.Kill(context.Background(), coord); err != nil {
			t.Fatalf("kill coordinator: %v", err)
		}
		for i := 0; i < 2; i++ {
			if err := p.runDue(); err != nil {
				t.Fatalf("delivery pass: %v", err)
			}
		}
		if got := p.deliv.receivedBy(p.root.ID, "coordinator_terminated"); got != 1 {
			t.Fatalf("root received %d escalations, want 1", got)
		}
	})

	t.Run("immediate, retried after a failed delivery, and not duplicated by a second kill", func(t *testing.T) {
		p := newPilot(t)
		ctx := context.Background()
		coord := p.mustSpawn(domain.KindCoordinator, p.root.ID, "auth area")
		worker := p.mustSpawn(domain.KindWorker, coord, "")
		if _, err := p.svc.Kill(ctx, coord); err != nil {
			t.Fatalf("kill coordinator: %v", err)
		}
		// No clock advance: an escalation skips the summary cadence.
		p.deliv.setFail(errors.New("root pane unavailable"))
		_ = p.runDue()
		if got := p.deliv.receivedBy(p.root.ID, "coordinator_terminated"); got != 0 {
			t.Fatalf("a failed delivery was recorded as accepted %d times", got)
		}
		p.deliv.setFail(nil)
		p.advance(time.Minute)
		if err := p.runDue(); err != nil {
			t.Fatalf("retry pass: %v", err)
		}
		// A second termination must not write another escalation.
		_, _ = p.svc.Kill(ctx, coord)
		p.advance(time.Minute)
		for i := 0; i < 2; i++ {
			_ = p.runDue()
		}
		if got := p.deliv.receivedBy(p.root.ID, "coordinator_terminated"); got != 1 {
			t.Fatalf("root received %d escalations after retry and a second kill, want 1", got)
		}
		if got := p.deliv.receivedBy(p.root.ID, string(worker)); got != 1 {
			t.Fatalf("the escalation does not list the returned worker %s", worker)
		}
		if p.ownerOf(worker) != "" {
			t.Fatalf("worker owner = %q, want the root", p.ownerOf(worker))
		}
	})
}

func mustReports(t *testing.T, p *pilot) []domain.ReportRecord {
	t.Helper()
	recs, err := p.store.ListReportsByProject(context.Background(), project)
	if err != nil {
		t.Fatalf("list reports: %v", err)
	}
	return recs
}

func reportByNote(t *testing.T, p *pilot, note string) domain.ReportRecord {
	t.Helper()
	for _, r := range mustReports(t, p) {
		if r.Note == note {
			return r
		}
	}
	t.Fatalf("no report with note %q", note)
	return domain.ReportRecord{}
}
