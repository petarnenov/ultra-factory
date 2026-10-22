package report

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// stuckWakeStore builds a root and a coordinator that owns worker-a. The
// coordinator is hibernated when asked, the way an idle chat controller is.
func stuckWakeStore(now time.Time, hibernated bool) *coordinatorStore {
	store := newCoordinatorStore(now)
	owner := domain.SessionID("coord-a")
	coordinator := domain.SessionRecord{ID: "coord-a", ProjectID: "p", Kind: domain.KindCoordinator, CoordinatorScope: "auth", Mode: domain.SessionModeChat, CreatedAt: now}
	if hibernated {
		since := now.Add(-time.Hour)
		coordinator.HibernatedAt = &since
	}
	store.sessions = []domain.SessionRecord{
		{ID: "root", ProjectID: "p", Kind: domain.KindOrchestrator, Mode: domain.SessionModeChat, CreatedAt: now},
		coordinator,
		{ID: "worker-a", ProjectID: "p", Kind: domain.KindWorker, OwnerSessionID: &owner},
	}
	return store
}

func stuckWakeReport(id string, state domain.ReportState, at time.Time, note string) domain.ReportRecord {
	report := reportAt(id, "worker-a", at, at, note)
	report.State = state
	return report
}

func TestCoordinatorStuckReportWakesHibernatedCoordinator(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	for _, occupied := range []bool{false, true} {
		store := stuckWakeStore(now, true)
		if occupied {
			store.interrupts["worker-a"] = now
		}
		store.add(stuckWakeReport("stuck", domain.ReportStuck, now, "cannot continue"))
		delivery := &coordinatorDelivery{}
		c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery, Now: func() time.Time { return now }})

		if _, err := c.RunDue(context.Background()); err != nil {
			t.Fatalf("occupied=%v: %v", occupied, err)
		}
		if len(delivery.interrupts) != 0 {
			t.Fatalf("occupied=%v: hibernated coordinator was interrupted: %v", occupied, delivery.interrupts)
		}
		if len(delivery.submissions) != 1 || delivery.submissions[0].target != "coord-a" {
			t.Fatalf("occupied=%v: submissions = %#v, want one wake of coord-a", occupied, delivery.submissions)
		}
		if !strings.Contains(delivery.submissions[0].message, "cannot continue") {
			t.Fatalf("occupied=%v: message = %q", occupied, delivery.submissions[0].message)
		}
		if store.acked != 1 {
			t.Fatalf("occupied=%v: acknowledged = %d, want the batch consumed once", occupied, store.acked)
		}
	}
}

func TestCoordinatorStuckBatchInterruptSlotMatrix(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name            string
		extra           []domain.ReportState
		slotOccupied    bool
		wantInterrupts  int
		wantSubmissions int
	}{
		{name: "stuck only, slot free", slotOccupied: false, wantInterrupts: 1, wantSubmissions: 1},
		{name: "stuck only, slot occupied defers", slotOccupied: true, wantInterrupts: 0, wantSubmissions: 0},
		{name: "stuck and needs-input, slot free", extra: []domain.ReportState{domain.ReportNeedsInput}, wantInterrupts: 1, wantSubmissions: 1},
		{name: "stuck and needs-input, slot occupied", extra: []domain.ReportState{domain.ReportNeedsInput}, slotOccupied: true, wantSubmissions: 1},
		{name: "stuck and done, slot free", extra: []domain.ReportState{domain.ReportDone}, wantInterrupts: 1, wantSubmissions: 1},
		{name: "stuck and done, slot occupied", extra: []domain.ReportState{domain.ReportDone}, slotOccupied: true, wantSubmissions: 1},
		{name: "stuck and checkpoint, slot free", extra: []domain.ReportState{domain.ReportCheckpoint}, wantInterrupts: 1, wantSubmissions: 1},
		{name: "stuck and checkpoint, slot occupied", extra: []domain.ReportState{domain.ReportCheckpoint}, slotOccupied: true, wantSubmissions: 1},
		{name: "stuck, needs-input and done, slot occupied", extra: []domain.ReportState{domain.ReportNeedsInput, domain.ReportDone}, slotOccupied: true, wantSubmissions: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := stuckWakeStore(now, false)
			if tc.slotOccupied {
				store.interrupts["worker-a"] = now
			}
			store.add(stuckWakeReport("stuck", domain.ReportStuck, now, "cannot continue"))
			for i, state := range tc.extra {
				store.add(stuckWakeReport(fmt.Sprintf("extra-%d", i), state, now.Add(time.Second), "extra note"))
			}
			delivery := &coordinatorDelivery{}
			c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery, Now: func() time.Time { return now }})

			if _, err := c.RunDue(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(delivery.interrupts) != tc.wantInterrupts {
				t.Fatalf("interrupts = %v, want %d", delivery.interrupts, tc.wantInterrupts)
			}
			if len(delivery.submissions) != tc.wantSubmissions {
				t.Fatalf("submissions = %#v, want %d", delivery.submissions, tc.wantSubmissions)
			}
		})
	}
}

func TestCoordinatorFailedInterruptNeverConsumesTheSlot(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	store := stuckWakeStore(now, false)
	store.add(stuckWakeReport("stuck", domain.ReportStuck, now, "cannot continue"))
	// The live coordinator reports no controller: this is not a wake, so it
	// must surface, must not submit, and must leave the slot free for a retry.
	delivery := &coordinatorDelivery{interruptErr: errors.New("no live chat controller for session")}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery, Now: func() time.Time { return now }})

	if _, err := c.RunDue(context.Background()); err == nil {
		t.Fatal("failed interrupt returned no error")
	}
	if len(delivery.submissions) != 0 {
		t.Fatalf("submitted after a failed interrupt: %#v", delivery.submissions)
	}
	if _, held := store.interrupts["worker-a"]; held {
		t.Fatal("failed interrupt consumed the per-worker interrupt slot")
	}

	delivery.interruptErr = nil
	if _, err := c.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(delivery.interrupts) != 2 || len(delivery.submissions) != 1 {
		t.Fatalf("retry interrupts = %v, submissions = %#v", delivery.interrupts, delivery.submissions)
	}
}
