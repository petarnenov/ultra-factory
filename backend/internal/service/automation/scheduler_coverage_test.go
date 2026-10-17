package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

type schedulerCoverageStore struct {
	*schedulerMemoryStore
	activeErr  error
	dueErr     error
	claimErr   error
	getErr     error
	claimCalls int
}

func (f *schedulerCoverageStore) ListActiveAutomationRuns(ctx context.Context) ([]domain.AutomationRun, error) {
	if f.activeErr != nil {
		return nil, f.activeErr
	}
	return f.schedulerMemoryStore.ListActiveAutomationRuns(ctx)
}

func (f *schedulerCoverageStore) ListDueAutomations(ctx context.Context, now time.Time, limit int64) ([]domain.Automation, error) {
	if f.dueErr != nil {
		return nil, f.dueErr
	}
	return f.schedulerMemoryStore.ListDueAutomations(ctx, now, limit)
}

func (f *schedulerCoverageStore) ClaimNextAutomationRun(ctx context.Context, now, lease time.Time) (domain.AutomationRun, bool, error) {
	f.claimCalls++
	if f.claimErr != nil {
		return domain.AutomationRun{}, false, f.claimErr
	}
	return f.schedulerMemoryStore.ClaimNextAutomationRun(ctx, now, lease)
}

func (f *schedulerCoverageStore) GetAutomation(ctx context.Context, id domain.AutomationID) (domain.Automation, bool, error) {
	if f.getErr != nil {
		return domain.Automation{}, false, f.getErr
	}
	return f.schedulerMemoryStore.GetAutomation(ctx, id)
}

func TestSchedulerTickReportsStoreErrorsAndContinues(t *testing.T) {
	now := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	activeErr := errors.New("active run read failed")
	dueErr := errors.New("due definition read failed")
	claimErr := errors.New("claim failed")
	store := &schedulerCoverageStore{
		schedulerMemoryStore: newSchedulerStore(),
		activeErr:            activeErr,
		dueErr:               dueErr,
		claimErr:             claimErr,
	}
	svc := New(Deps{Store: store, Clock: func() time.Time { return now }})

	err := svc.Tick(context.Background(), now)
	for _, want := range []error{activeErr, dueErr, claimErr} {
		if !errors.Is(err, want) {
			t.Errorf("Tick error %v does not include %v", err, want)
		}
	}
	if store.claimCalls != 1 {
		t.Fatalf("claim calls = %d, want 1", store.claimCalls)
	}
}

func TestSchedulerTickCancellationStopsClaimingAndIsReturned(t *testing.T) {
	now := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	store := &schedulerCoverageStore{schedulerMemoryStore: newSchedulerStore()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc := New(Deps{Store: store, Clock: func() time.Time { return now }})

	err := svc.Tick(ctx, now)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Tick error = %v, want context.Canceled", err)
	}
	if store.claimCalls != 0 {
		t.Fatalf("claim calls = %d, want 0 after cancellation", store.claimCalls)
	}
}

func TestSchedulerTickCapsClaimsBeforeDispatch(t *testing.T) {
	now := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	store := newSchedulerStore()
	for i := 0; i < dispatchScanLimit+1; i++ {
		automationID := domain.AutomationID(fmt.Sprintf("automation-%03d", i))
		runID := domain.AutomationRunID(fmt.Sprintf("run-%03d", i))
		store.automations[automationID] = domain.Automation{ID: automationID, Enabled: true, NextRunAt: now.Add(time.Hour)}
		store.runs[runID] = domain.AutomationRun{ID: runID, AutomationID: automationID, ScheduledFor: now, Status: domain.AutomationRunPending}
	}
	spawner := &recordingSpawner{store: store}
	svc := New(Deps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	if err := svc.Tick(context.Background(), now); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(spawner.calls) != dispatchScanLimit {
		t.Fatalf("spawn calls = %d, want dispatch limit %d", len(spawner.calls), dispatchScanLimit)
	}
	var pending int
	for _, run := range store.runs {
		if run.Status == domain.AutomationRunPending {
			pending++
		}
	}
	if pending != 1 {
		t.Fatalf("pending runs = %d, want 1 left beyond the claim limit", pending)
	}
}

func TestSchedulerTickReleasesClaimWhenSpawnerUnavailable(t *testing.T) {
	now := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	store := newSchedulerStore()
	store.automations["automation-1"] = domain.Automation{ID: "automation-1", Enabled: true, NextRunAt: now.Add(time.Hour)}
	store.runs["run-1"] = domain.AutomationRun{ID: "run-1", AutomationID: "automation-1", ScheduledFor: now, Status: domain.AutomationRunPending}
	svc := New(Deps{Store: store, Clock: func() time.Time { return now }})

	err := svc.Tick(context.Background(), now)
	if err == nil || !strings.Contains(err.Error(), "session spawner is unavailable") {
		t.Fatalf("Tick error = %v, want unavailable spawner diagnostic", err)
	}
	got := store.runs["run-1"]
	if got.Status != domain.AutomationRunPending || got.ErrorMessage == "" || got.ClaimedAt != nil || got.LeaseExpiresAt != nil {
		t.Fatalf("run = %#v, want released pending claim with diagnostic", got)
	}
}

func TestSchedulerTickFailsRunWhenAutomationDefinitionReadFails(t *testing.T) {
	now := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	memStore := newSchedulerStore()
	memStore.automations["automation-1"] = domain.Automation{ID: "automation-1", Enabled: true, NextRunAt: now.Add(time.Hour)}
	memStore.runs["run-1"] = domain.AutomationRun{ID: "run-1", AutomationID: "automation-1", ScheduledFor: now, Status: domain.AutomationRunPending}
	store := &schedulerCoverageStore{schedulerMemoryStore: memStore, getErr: errors.New("definition read failed")}
	spawner := &recordingSpawner{store: memStore}
	svc := New(Deps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	err := svc.Tick(context.Background(), now)
	if err == nil {
		t.Fatal("Tick succeeded, want definition-read diagnostic")
	}
	if got := memStore.runs["run-1"].Status; got != domain.AutomationRunFailed {
		t.Fatalf("run status = %s, want failed", got)
	}
	if len(spawner.calls) != 0 {
		t.Fatalf("spawn calls = %d, want 0", len(spawner.calls))
	}
}

func TestSchedulerTickFailsPermanentlyInvalidSpawn(t *testing.T) {
	now := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	store := newSchedulerStore()
	store.automations["automation-1"] = domain.Automation{ID: "automation-1", ProjectID: "scheduled", Enabled: true, NextRunAt: now.Add(time.Hour)}
	store.runs["run-1"] = domain.AutomationRun{ID: "run-1", AutomationID: "automation-1", ScheduledFor: now, Status: domain.AutomationRunPending}
	spawner := &recordingSpawner{store: store, spawnErr: apierr.Invalid("INVALID_CONFIG", "bad launch configuration", nil)}
	svc := New(Deps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	err := svc.Tick(context.Background(), now)
	if err == nil {
		t.Fatal("Tick succeeded, want spawn error")
	}
	got := store.runs["run-1"]
	if got.Status != domain.AutomationRunFailed || got.ErrorMessage == "" || got.FinishedAt == nil {
		t.Fatalf("run = %#v, want terminal failed run", got)
	}
}

func TestSchedulerReconcileRejectsStoreWithoutSchedulerOperations(t *testing.T) {
	svc := New(Deps{Store: newFakeStore()})
	if err := svc.Reconcile(context.Background()); err == nil || !strings.Contains(err.Error(), "scheduler storage is unavailable") {
		t.Fatalf("Reconcile error = %v, want unavailable scheduler storage", err)
	}
}
