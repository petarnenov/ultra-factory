package report

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func exitedCoordinatorStore(now time.Time, owner domain.SessionID) *coordinatorStore {
	store := newCoordinatorStore(now)
	store.sessions = []domain.SessionRecord{
		{ID: "root", ProjectID: "p", Kind: domain.KindOrchestrator, Mode: domain.SessionModeChat, CreatedAt: now},
		{ID: "coord-exited", ProjectID: "p", Kind: domain.KindCoordinator, CoordinatorScope: "auth", Activity: domain.Activity{State: domain.ActivityExited}, CreatedAt: now},
		{ID: "worker", ProjectID: "p", Kind: domain.KindWorker, OwnerSessionID: &owner},
	}
	return store
}

func TestCoordinatorRoutesAnExitedOwnersReportToRootOnce(t *testing.T) {
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	owner := domain.SessionID("coord-exited")
	store := exitedCoordinatorStore(now, owner)
	store.add(reportAt("report", "worker", now, now, "done"))
	delivery := &coordinatorDelivery{}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery, Now: func() time.Time { return now }})

	if _, err := c.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 2 {
		t.Fatalf("submissions = %#v, want a recovery notice and the report", delivery.submissions)
	}
	for _, sub := range delivery.submissions {
		if sub.target != "root" {
			t.Fatalf("submission to %s, want root", sub.target)
		}
	}
	if store.acked != 1 {
		t.Fatalf("acknowledged = %d, want 1", store.acked)
	}
	if len(store.ownerEvents) != 0 {
		t.Fatalf("exited owner was repaired: %#v", store.ownerEvents)
	}

	store.add(reportAt("again", "worker", now, now, "more"))
	if _, err := c.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 3 {
		t.Fatalf("submissions = %d, want the recovery notice sent once", len(delivery.submissions))
	}
}

func TestCoordinatorRearmsTheRecoveryNoticeAfterRestore(t *testing.T) {
	now := time.Date(2026, 10, 20, 9, 5, 0, 0, time.UTC)
	owner := domain.SessionID("coord-exited")
	store := exitedCoordinatorStore(now, owner)
	delivery := &coordinatorDelivery{}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery, Now: func() time.Time { return now }})
	run := func(id string) {
		t.Helper()
		store.add(reportAt(id, "worker", now, now, id))
		if _, err := c.RunDue(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	run("one")
	store.sessions[1].Activity.State = domain.ActivityActive
	run("two")
	if got := delivery.submissions[len(delivery.submissions)-1].target; got != "coord-exited" {
		t.Fatalf("restored coordinator got no report; last target %s", got)
	}
	store.sessions[1].Activity = domain.Activity{State: domain.ActivityExited, LastActivityAt: now.Add(time.Hour)}
	before := len(delivery.submissions)
	run("three")
	if len(delivery.submissions)-before != 2 {
		t.Fatalf("second exit sent %d submissions, want notice and report", len(delivery.submissions)-before)
	}
	keys := map[string]bool{}
	for _, sub := range delivery.submissions {
		if strings.HasPrefix(sub.key, "coordinator-exited:") {
			if keys[sub.key] {
				t.Fatalf("recovery notice id %s reused; a deduping chat drops the second exit", sub.key)
			}
			keys[sub.key] = true
		}
	}
	if len(keys) != 2 {
		t.Fatalf("notice ids = %v, want one per exit", keys)
	}
}

func TestCoordinatorTellsAReplacementRootAboutAnExitedOwner(t *testing.T) {
	now := time.Date(2026, 10, 20, 9, 7, 0, 0, time.UTC)
	owner := domain.SessionID("coord-exited")
	store := exitedCoordinatorStore(now, owner)
	delivery := &coordinatorDelivery{}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery, Now: func() time.Time { return now }})
	store.add(reportAt("one", "worker", now, now, "one"))
	if _, err := c.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.sessions[0].IsTerminated = true
	store.sessions = append(store.sessions, domain.SessionRecord{ID: "root2", ProjectID: "p", Kind: domain.KindOrchestrator, Mode: domain.SessionModeChat, CreatedAt: now.Add(time.Minute)})
	store.add(reportAt("two", "worker", now, now, "two"))
	if _, err := c.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	notices := 0
	for _, sub := range delivery.submissions {
		if sub.target == "root2" && strings.HasPrefix(sub.key, "coordinator-exited:") {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("replacement root got %d notices, want 1: %#v", notices, delivery.submissions)
	}
}

func TestResolveRemoteRecipientRoutesAroundAnExitedOwner(t *testing.T) {
	now := time.Date(2026, 10, 20, 9, 10, 0, 0, time.UTC)
	owner := domain.SessionID("coord-exited")
	store := exitedCoordinatorStore(now, owner)
	store.seedLink(&owner)
	delivery := &coordinatorDelivery{}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery, Now: func() time.Time { return now }})
	link := domain.RemoteWorkerLink{HostID: "host", RemoteSessionID: "widgets-3", ProjectID: "p", OwnerSessionID: &owner}
	for i := 0; i < 2; i++ {
		rec, ok, err := c.ResolveRemoteRecipient(context.Background(), link)
		if err != nil || !ok || rec.ID != "root" {
			t.Fatalf("recipient = %+v ok=%v err=%v", rec, ok, err)
		}
	}
	if len(store.ownerEvents) != 0 {
		t.Fatalf("exited owner was repaired: %#v", store.ownerEvents)
	}
	if len(delivery.submissions) != 1 || delivery.submissions[0].target != "root" {
		t.Fatalf("recovery notices = %#v, want one to root", delivery.submissions)
	}
}

func TestDeliverExternalRejectsAnExitedCoordinator(t *testing.T) {
	now := time.Date(2026, 10, 20, 9, 15, 0, 0, time.UTC)
	store := exitedCoordinatorStore(now, "coord-exited")
	delivery := &coordinatorDelivery{}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery, Now: func() time.Time { return now }})
	err := c.DeliverExternal(context.Background(), "p", "coord-exited", []domain.ReportRecord{reportAt("r", "w", now, now, "x")})
	if !errors.Is(err, ErrRecipientUnavailable) || len(delivery.submissions) != 0 {
		t.Fatalf("err = %v submissions = %#v", err, delivery.submissions)
	}
}
