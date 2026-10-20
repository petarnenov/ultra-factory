package daemon

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/cdc"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/report"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

type recordingDelivery struct {
	mu   sync.Mutex
	sent []string
}

func (d *recordingDelivery) Submit(_ context.Context, target domain.SessionID, message, _ string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sent = append(d.sent, string(target)+"|"+message)
	return nil
}

func (d *recordingDelivery) Interrupt(context.Context, domain.SessionID) error { return nil }

func (d *recordingDelivery) count(target domain.SessionID, needle string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, s := range d.sent {
		if strings.HasPrefix(s, string(target)+"|") && strings.Contains(s, needle) {
			n++
		}
	}
	return n
}

// TestKillingCoordinatorWakesIdleReportLoop runs the real Start loop and CDC
// poller; nothing calls Wake except the termination wiring.
func TestKillingCoordinatorWakesIdleReportLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	const project = domain.ProjectID("ao")
	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: string(project), Path: "/tmp/ao-wake", RegisteredAt: now,
		Config: domain.ProjectConfig{Coordinators: domain.CoordinatorsConfig{Enabled: true}}}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	mk := func(kind domain.SessionKind, owner *domain.SessionID, scope string) domain.SessionRecord {
		rec, err := store.CreateSession(ctx, domain.SessionRecord{
			ProjectID: project, Kind: kind, OwnerSessionID: owner, CoordinatorScope: scope, Harness: domain.HarnessClaudeCode,
			Activity:  domain.Activity{State: domain.ActivityActive, LastActivityAt: now},
			CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			t.Fatalf("create %s: %v", kind, err)
		}
		return rec
	}
	root := mk(domain.KindOrchestrator, nil, "")
	coord := mk(domain.KindCoordinator, nil, "auth area")
	worker := mk(domain.KindWorker, &coord.ID, "")

	bcast := cdc.NewBroadcaster()
	poller := cdc.NewPoller(store, bcast, cdc.PollerConfig{Interval: 20 * time.Millisecond})
	if err := poller.SeekToHead(ctx); err != nil {
		t.Fatalf("seek: %v", err)
	}
	pollDone := poller.Start(ctx)

	deliv := &recordingDelivery{}
	coordinator := report.NewCoordinator(report.CoordinatorDeps{Store: store, Delivery: deliv, Summaries: store, RetryDelay: 50 * time.Millisecond})
	defer wakeOnCoordinatorTermination(ctx, bcast, store, coordinator.Wake)()
	loopDone := coordinator.Start(ctx)
	defer func() { cancel(); <-loopDone; <-pollDone }()
	time.Sleep(100 * time.Millisecond) // let the loop go idle

	// No OnCreated hook: the report sits pending until the kill wakes the loop.
	reports := report.New(report.Deps{Store: store})
	if _, err := reports.Create(ctx, report.CreateInput{SessionID: worker.ID, State: domain.ReportNeedsInput, Note: "WORKER-NOTE"}); err != nil {
		t.Fatalf("report: %v", err)
	}
	rec := coord
	rec.IsTerminated = true
	rec.UpdatedAt = time.Now().UTC()
	if err := store.UpdateSession(ctx, rec); err != nil {
		t.Fatalf("kill coordinator: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if deliv.count(root.ID, "coordinator_terminated") == 1 && deliv.count(root.ID, "WORKER-NOTE") >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("root did not receive escalation and report in time; sent = %v", deliv.sent)
}

func TestWorkerTerminationDoesNotWakeReportLoop(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "ao", Path: "/tmp/ao-wake2", RegisteredAt: now}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	worker, err := store.CreateSession(ctx, domain.SessionRecord{ProjectID: "ao", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode,
		Activity: domain.Activity{State: domain.ActivityActive, LastActivityAt: now}, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	bcast := cdc.NewBroadcaster()
	woke := 0
	defer wakeOnCoordinatorTermination(ctx, bcast, store, func() { woke++ })()
	payload := []byte(`{"isTerminated":true}`)
	bcast.Publish(cdc.Event{Type: cdc.EventSessionUpdated, SessionID: string(worker.ID), Payload: payload})
	if woke != 0 {
		t.Fatalf("worker termination woke the loop %d times", woke)
	}
}
