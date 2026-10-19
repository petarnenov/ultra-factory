package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestCoordinatorSummaryClaimsAreGroupScopedAndTokenFenced(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "ao")
	seedProject(t, s, "other")
	now := time.Now().UTC().Truncate(time.Second)
	coordA, err := s.CreateSession(ctx, coordinatorRecord("ao", "a"))
	if err != nil {
		t.Fatal(err)
	}
	coordB, err := s.CreateSession(ctx, coordinatorRecord("ao", "b"))
	if err != nil {
		t.Fatal(err)
	}
	ownedRec := sampleRecord("ao")
	ownedRec.OwnerSessionID = &coordA.ID
	worker, err := s.CreateSession(ctx, ownedRec)
	if err != nil {
		t.Fatal(err)
	}
	root := sampleRecord("ao")
	root.Kind = domain.KindOrchestrator
	rootRec, err := s.CreateSession(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	mk := func(id string, coord domain.SessionID, key string, workers ...domain.SessionID) domain.CoordinatorSummary {
		rec, created, err := s.CreateCoordinatorSummary(ctx, domain.CoordinatorSummary{
			ID: id, CoordinatorSessionID: coord, ProjectID: "ao", SubmissionKey: key, Scope: "scope",
			State: domain.SummaryWorking, WorkerSessionIDs: workers, CreatedAt: now, AvailableAt: now,
		})
		if err != nil || !created {
			t.Fatalf("create %s: created=%v err=%v", id, created, err)
		}
		return rec
	}
	mk("s1", coordA.ID, "k1", worker.ID)
	mk("s2", coordA.ID, "k2")
	mk("s3", coordB.ID, "k1")

	dup, created, err := s.CreateCoordinatorSummary(ctx, domain.CoordinatorSummary{
		ID: "s-dup", CoordinatorSessionID: coordA.ID, ProjectID: "ao", SubmissionKey: "k1", Scope: "scope",
		State: domain.SummaryDone, CreatedAt: now, AvailableAt: now,
	})
	if err != nil || created || dup.ID != "s1" || len(dup.WorkerSessionIDs) != 1 {
		t.Fatalf("duplicate = %+v created=%v err=%v, want the original s1", dup, created, err)
	}

	claimed, err := s.ClaimPendingCoordinatorSummaryBatch(ctx, "ao", coordA.ID, rootRec.ID, "tok-a", now)
	if err != nil || len(claimed) != 2 {
		t.Fatalf("claim A = %+v err=%v, want only coordinator A's two summaries", claimed, err)
	}
	for _, c := range claimed {
		if c.CoordinatorSessionID != coordA.ID || c.DeliveryOwnerID != string(rootRec.ID) || c.DeliveryAttempts != 1 || c.DeliveryBatchID == "" {
			t.Fatalf("claimed row = %+v", c)
		}
	}
	if again, err := s.ClaimPendingCoordinatorSummaryBatch(ctx, "ao", coordA.ID, rootRec.ID, "tok-2", now); err != nil || len(again) != 0 {
		t.Fatalf("second claim = %+v err=%v, want none while claimed", again, err)
	}
	if n, err := s.AcknowledgeCoordinatorSummaryBatch(ctx, "ao", "wrong-token", now); err != nil || n != 0 {
		t.Fatalf("ack with the wrong token n=%d err=%v", n, err)
	}
	if n, err := s.AcknowledgeCoordinatorSummaryBatch(ctx, "other", "tok-a", now); err != nil || n != 0 {
		t.Fatalf("ack in another project n=%d err=%v", n, err)
	}
	if n, err := s.AcknowledgeCoordinatorSummaryBatch(ctx, "ao", "tok-a", now); err != nil || n != 2 {
		t.Fatalf("ack n=%d err=%v", n, err)
	}
	if n, err := s.ReleaseCoordinatorSummaryBatch(ctx, "ao", "tok-a", now, "late"); err != nil || n != 0 {
		t.Fatalf("release after ack n=%d err=%v, an acknowledged summary is terminal", n, err)
	}
	pending, err := s.ListPendingCoordinatorSummarySchedule(ctx, 10)
	if err != nil || len(pending) != 1 || pending[0].ID != "s3" {
		t.Fatalf("pending = %+v err=%v, want only coordinator B's summary", pending, err)
	}
	latest, ok, err := s.GetLatestAcceptedCoordinatorSummary(ctx, coordA.ID)
	if err != nil || !ok || (latest.ID != "s1" && latest.ID != "s2") {
		t.Fatalf("latest accepted = %+v ok=%v err=%v", latest, ok, err)
	}
	if _, ok, _ := s.GetLatestAcceptedCoordinatorSummary(ctx, coordB.ID); ok {
		t.Fatal("an unacknowledged summary was reported as accepted")
	}
}

func TestCoordinatorSummaryHistoryIsRetainedWhenWorkersAreReassigned(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "ao")
	now := time.Now().UTC().Truncate(time.Second)
	coord, err := s.CreateSession(ctx, coordinatorRecord("ao", "a"))
	if err != nil {
		t.Fatal(err)
	}
	ownedRec := sampleRecord("ao")
	ownedRec.OwnerSessionID = &coord.ID
	worker, err := s.CreateSession(ctx, ownedRec)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateCoordinatorSummary(ctx, domain.CoordinatorSummary{
		ID: "s1", CoordinatorSessionID: coord.ID, ProjectID: "ao", SubmissionKey: "k", Scope: "a", State: domain.SummaryDone,
		WorkerSessionIDs: []domain.SessionID{worker.ID}, CreatedAt: now, AvailableAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SetSessionOwner(ctx, worker.ID, nil, domain.OwnershipOwnerNotLiveReconciled, now.Add(time.Minute), true); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListCoordinatorSummaries(ctx, coord.ID, 10)
	if err != nil || len(got) != 1 || len(got[0].WorkerSessionIDs) != 1 || got[0].WorkerSessionIDs[0] != worker.ID {
		t.Fatalf("history = %+v err=%v, want the worker link kept after reassignment", got, err)
	}
	if _, _, err := s.CreateCoordinatorSummary(ctx, domain.CoordinatorSummary{
		ID: "bad", CoordinatorSessionID: coord.ID, ProjectID: "ao", SubmissionKey: "k2", Scope: "a", State: domain.SummaryWorking,
		EscalationReason: domain.EscalationWorkerFailed, CreatedAt: now, AvailableAt: now,
	}); err == nil {
		t.Fatal("an escalation reason on a non-blocked summary was stored")
	}
}
