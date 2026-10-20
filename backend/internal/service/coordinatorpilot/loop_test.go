package coordinatorpilot_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// start runs the real scheduler loop; the clock stays fake, the timers do not.
func (p *pilot) start() {
	p.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := p.coord.Start(ctx)
	p.t.Cleanup(func() {
		cancel()
		<-done
	})
}

// rewire rebuilds the services, which discards any wake queued before the loop.
func (p *pilot) rewire() { p.wire() }

// eventually polls cond for a few seconds; the loop has no other signal.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// settle gives the loop time to misdeliver before a count is re-checked.
func settle() { time.Sleep(150 * time.Millisecond) }

func TestLoopNewReportWakesAnIdleScheduler(t *testing.T) {
	p := newPilot(t)
	worker := p.mustSpawn(domain.KindWorker, p.root.ID, "")
	p.start()
	// Nothing is pending, so the loop sleeps its idle hour; only the wake
	// from report creation can deliver this.
	settle()
	note := p.reportFrom(worker, "wake")
	eventually(t, "the new report to reach the root", func() bool { return p.deliv.receivedBy(p.root.ID, note) == 1 })
	settle()
	if got := p.deliv.receivedBy(p.root.ID, note); got != 1 {
		t.Fatalf("root received the report %d times, want 1", got)
	}
}

func TestLoopCoordinatorTerminationWakesAnIdleScheduler(t *testing.T) {
	p := newPilot(t)
	coord := p.mustSpawn(domain.KindCoordinator, p.root.ID, "auth area")
	worker := p.mustSpawn(domain.KindWorker, coord, "")
	p.start()
	settle()
	if _, err := p.svc.Kill(context.Background(), coord); err != nil {
		t.Fatalf("kill coordinator: %v", err)
	}
	// The termination trigger writes the escalation row with no create hook;
	// the daemon wakes the loop from the session update, as done here.
	p.coord.Wake()
	eventually(t, "the escalation to reach the root", func() bool { return p.deliv.receivedBy(p.root.ID, "coordinator_terminated") == 1 })
	if got := p.deliv.receivedBy(p.root.ID, string(worker)); got != 1 {
		t.Fatalf("the escalation does not list the returned worker %s", worker)
	}
	settle()
	if got := p.deliv.receivedBy(p.root.ID, "coordinator_terminated"); got != 1 {
		t.Fatalf("root received %d escalations, want 1", got)
	}
}

func TestLoopRetriesADueReportWithoutAnExternalWake(t *testing.T) {
	p := newPilot(t)
	worker := p.mustSpawn(domain.KindWorker, p.root.ID, "")
	p.deliv.setFail(errors.New("root pane unavailable"))
	p.start()
	note := p.reportFrom(worker, "retry")
	eventually(t, "a first failed attempt", func() bool {
		p.deliv.mu.Lock()
		defer p.deliv.mu.Unlock()
		return len(p.deliv.attempts) > 0
	})
	// No Wake from here on: the retry delay alone must redeliver.
	p.deliv.setFail(nil)
	eventually(t, "the retry to deliver", func() bool { return p.deliv.receivedBy(p.root.ID, note) == 1 })
	settle()
	if got := p.deliv.receivedBy(p.root.ID, note); got != 1 {
		t.Fatalf("root received the report %d times, want 1", got)
	}
}

func TestLoopDeliversEveryDueReportAcrossBatchesWithoutAWake(t *testing.T) {
	p := newPilot(t)
	coord := p.mustSpawn(domain.KindCoordinator, p.root.ID, "auth area")
	owned := p.mustSpawn(domain.KindWorker, coord, "")
	rooted := p.mustSpawn(domain.KindWorker, p.root.ID, "")
	// Queue reports for two recipients before the loop runs, then start it:
	// the second group is a leftover of the first pass and nothing wakes it.
	first := p.reportFrom(owned, "a")
	second := p.reportFrom(rooted, "b")
	p.rewire() // drop the wakes queued by the two reports above
	p.start()
	eventually(t, "both recipients to receive their report", func() bool {
		return p.deliv.receivedBy(coord, first) == 1 && p.deliv.receivedBy(p.root.ID, second) == 1
	})
	settle()
	if p.deliv.receivedBy(coord, first) != 1 || p.deliv.receivedBy(p.root.ID, second) != 1 || p.deliv.receivedBy(p.root.ID, first) != 0 {
		t.Fatal("a report was duplicated or misrouted")
	}
}

func TestLoopDeliversLeftoverReportsOfAnExitedNotTerminatedCoordinatorToTheRoot(t *testing.T) {
	p := newPilot(t)
	coord := p.mustSpawn(domain.KindCoordinator, p.root.ID, "auth area")
	worker := p.mustSpawn(domain.KindWorker, coord, "")
	note := p.reportFrom(worker, "exited")
	rec := p.get(coord)
	rec.Activity.State = domain.ActivityExited
	rec.UpdatedAt = p.now()
	if err := p.store.UpdateSession(context.Background(), rec); err != nil {
		t.Fatalf("exit coordinator: %v", err)
	}
	p.start()
	eventually(t, "the root to receive the exited coordinator's report", func() bool { return p.deliv.receivedBy(p.root.ID, note) == 1 })
	settle()
	if p.deliv.receivedBy(coord, note) != 0 || p.deliv.receivedBy(p.root.ID, note) != 1 {
		t.Fatalf("report misrouted: coordinator=%d root=%d", p.deliv.receivedBy(coord, note), p.deliv.receivedBy(p.root.ID, note))
	}
}

// A batch left claimed-then-released keeps its batch id; a later report is
// outside it, so one pass cannot deliver both and nothing else wakes the loop.
func TestLoopDeliversTheLeftoverBatchAfterAClaimedOne(t *testing.T) {
	p := newPilot(t)
	ctx := context.Background()
	worker := p.mustSpawn(domain.KindWorker, p.root.ID, "")
	first := p.reportFrom(worker, "first")
	claimed, err := p.store.ClaimPendingReportBatch(ctx, project, p.root.ID, p.root.ID, "claim-earlier", p.now())
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %d reports, err %v", len(claimed), err)
	}
	if _, err := p.store.ReleaseReportBatch(ctx, project, "claim-earlier", p.now(), "released"); err != nil {
		t.Fatalf("release: %v", err)
	}
	second := p.reportFrom(worker, "second")
	p.rewire()
	p.start()
	eventually(t, "both batches to be delivered", func() bool {
		return p.deliv.receivedBy(p.root.ID, first) >= 1 && p.deliv.receivedBy(p.root.ID, second) >= 1
	})
	settle()
	if p.deliv.receivedBy(p.root.ID, first) != 1 || p.deliv.receivedBy(p.root.ID, second) != 1 {
		t.Fatalf("first delivered %d times, second %d, want once each", p.deliv.receivedBy(p.root.ID, first), p.deliv.receivedBy(p.root.ID, second))
	}
}
