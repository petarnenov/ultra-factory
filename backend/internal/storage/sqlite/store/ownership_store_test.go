package store_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func coordinatorRecord(project, scope string) domain.SessionRecord {
	rec := sampleRecord(project)
	rec.Kind = domain.KindCoordinator
	rec.CoordinatorScope = scope
	return rec
}

func TestSessionOwnerSurvivesRestartAndIsAudited(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	seedProject(t, s, "ao")
	coordinator, err := s.CreateSession(ctx, coordinatorRecord("ao", "auth area"))
	if err != nil {
		t.Fatal(err)
	}
	workerRec := sampleRecord("ao")
	workerRec.OwnerSessionID = &coordinator.ID
	worker, err := s.CreateSession(ctx, workerRec)
	if err != nil {
		t.Fatal(err)
	}
	if worker.OwnerSessionID == nil || *worker.OwnerSessionID != coordinator.ID {
		t.Fatalf("created owner = %v, want %s", worker.OwnerSessionID, coordinator.ID)
	}
	events, err := s.ListSessionOwnerEvents(ctx, worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Reason != domain.OwnershipSpawnParent ||
		events[0].PreviousOwnerSessionID != nil || events[0].NewOwnerSessionID == nil || *events[0].NewOwnerSessionID != coordinator.ID {
		t.Fatalf("spawn audit = %+v", events)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	got, found, err := reopened.GetSession(ctx, worker.ID)
	if err != nil || !found {
		t.Fatalf("reopen worker: found=%v err=%v", found, err)
	}
	if got.OwnerSessionID == nil || *got.OwnerSessionID != coordinator.ID || got.CoordinatorScope != "" {
		t.Fatalf("reopened worker = %+v", got)
	}
	gotCoordinator, found, err := reopened.GetSession(ctx, coordinator.ID)
	if err != nil || !found {
		t.Fatalf("reopen coordinator: found=%v err=%v", found, err)
	}
	if gotCoordinator.Kind != domain.KindCoordinator || gotCoordinator.CoordinatorScope != "auth area" || gotCoordinator.OwnerSessionID != nil {
		t.Fatalf("reopened coordinator = %+v", gotCoordinator)
	}
}

func TestSetSessionOwnerIsAtomicIdempotentAndAudited(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "ao")
	first, err := s.CreateSession(ctx, coordinatorRecord("ao", "one"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateSession(ctx, coordinatorRecord("ao", "two"))
	if err != nil {
		t.Fatal(err)
	}
	worker, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)

	assigned, changed, err := s.SetSessionOwner(ctx, worker.ID, &first.ID, domain.OwnershipExplicitAssignment, now, false)
	if err != nil || !changed || assigned.OwnerSessionID == nil || *assigned.OwnerSessionID != first.ID {
		t.Fatalf("assign: changed=%v owner=%v err=%v", changed, assigned.OwnerSessionID, err)
	}
	reassigned, changed, err := s.SetSessionOwner(ctx, worker.ID, &second.ID, domain.OwnershipExplicitReassignment, now, false)
	if err != nil || !changed || reassigned.OwnerSessionID == nil || *reassigned.OwnerSessionID != second.ID {
		t.Fatalf("reassign: changed=%v owner=%v err=%v", changed, reassigned.OwnerSessionID, err)
	}
	again, changed, err := s.SetSessionOwner(ctx, worker.ID, &second.ID, domain.OwnershipExplicitReassignment, now, false)
	if err != nil || changed || again.OwnerSessionID == nil || *again.OwnerSessionID != second.ID {
		t.Fatalf("repeat: changed=%v owner=%v err=%v", changed, again.OwnerSessionID, err)
	}
	rooted, changed, err := s.SetSessionOwner(ctx, worker.ID, nil, domain.OwnershipExplicitReassignment, now, false)
	if err != nil || !changed || rooted.OwnerSessionID != nil {
		t.Fatalf("clear: changed=%v owner=%v err=%v", changed, rooted.OwnerSessionID, err)
	}
	events, err := s.ListSessionOwnerEvents(ctx, worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("audit = %+v, want three transitions", events)
	}
	if events[0].Reason != domain.OwnershipExplicitAssignment || events[1].Reason != domain.OwnershipExplicitReassignment || events[2].Reason != domain.OwnershipExplicitReassignment {
		t.Fatalf("audit reasons = %+v", events)
	}
	if events[2].NewOwnerSessionID != nil || events[2].PreviousOwnerSessionID == nil || *events[2].PreviousOwnerSessionID != second.ID {
		t.Fatalf("root transition audit = %+v", events[2])
	}
}

func TestOwnershipInvariantsRejectInvalidOwners(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "ao")
	seedProject(t, s, "other")
	root, err := s.CreateSession(ctx, func() domain.SessionRecord {
		rec := sampleRecord("ao")
		rec.Kind = domain.KindOrchestrator
		return rec
	}())
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := s.CreateSession(ctx, coordinatorRecord("ao", "area"))
	if err != nil {
		t.Fatal(err)
	}
	terminated, err := s.CreateSession(ctx, func() domain.SessionRecord {
		rec := coordinatorRecord("ao", "ended")
		rec.IsTerminated = true
		return rec
	}())
	if err != nil {
		t.Fatal(err)
	}
	otherProject, err := s.CreateSession(ctx, coordinatorRecord("other", "elsewhere"))
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		owner *domain.SessionID
		want  string
	}{
		{"root is not an eligible owner", &root.ID, "live coordinator"},
		{"terminated coordinator", &terminated.ID, "live coordinator"},
		{"cross-project coordinator", &otherProject.ID, "same project"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := sampleRecord("ao")
			rec.OwnerSessionID = tc.owner
			if _, err := s.CreateSession(ctx, rec); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want a %q rejection", err, tc.want)
			}
		})
	}

	t.Run("a root is never owned", func(t *testing.T) {
		rec := sampleRecord("ao")
		rec.Kind = domain.KindOrchestrator
		rec.OwnerSessionID = &coordinator.ID
		if _, err := s.CreateSession(ctx, rec); err == nil || !strings.Contains(err.Error(), "only workers") {
			t.Fatalf("err = %v, want an only-workers rejection", err)
		}
	})

	t.Run("coordinator is never owned", func(t *testing.T) {
		rec := coordinatorRecord("ao", "area")
		rec.OwnerSessionID = &coordinator.ID
		if _, err := s.CreateSession(ctx, rec); err == nil || !strings.Contains(err.Error(), "only workers") {
			t.Fatalf("err = %v, want an only-workers rejection", err)
		}
	})
	t.Run("coordinator scope is required", func(t *testing.T) {
		rec := coordinatorRecord("ao", "   ")
		if _, err := s.CreateSession(ctx, rec); err == nil || !strings.Contains(err.Error(), "coordinator scope is required") {
			t.Fatalf("err = %v, want a scope rejection", err)
		}
	})
	t.Run("coordinator requires a project", func(t *testing.T) {
		rec := coordinatorRecord("", "area")
		if _, err := s.CreateSession(ctx, rec); err == nil || !strings.Contains(err.Error(), "requires a project") {
			t.Fatalf("err = %v, want a project rejection", err)
		}
	})
	t.Run("worker carries no coordinator scope", func(t *testing.T) {
		rec := sampleRecord("ao")
		rec.CoordinatorScope = "not mine"
		if _, err := s.CreateSession(ctx, rec); err == nil || !strings.Contains(err.Error(), "only valid for kind=coordinator") {
			t.Fatalf("err = %v, want a scope rejection", err)
		}
	})
}

func TestGenericUpdateDoesNotRewriteOwnership(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "ao")
	coordinator, err := s.CreateSession(ctx, coordinatorRecord("ao", "area"))
	if err != nil {
		t.Fatal(err)
	}
	rec := sampleRecord("ao")
	rec.OwnerSessionID = &coordinator.ID
	worker, err := s.CreateSession(ctx, rec)
	if err != nil {
		t.Fatal(err)
	}
	// A stale snapshot that predates the owner write must not clear it.
	stale := worker
	stale.OwnerSessionID = nil
	stale.DisplayName = "renamed"
	if err := s.UpdateSession(ctx, stale); err != nil {
		t.Fatal(err)
	}
	got, found, err := s.GetSession(ctx, worker.ID)
	if err != nil || !found {
		t.Fatalf("read worker: found=%v err=%v", found, err)
	}
	if got.OwnerSessionID == nil || *got.OwnerSessionID != coordinator.ID {
		t.Fatalf("owner after full update = %v, want %s", got.OwnerSessionID, coordinator.ID)
	}
}

func TestCoordinatorTerminationReleasesLocalAndRemoteWorkers(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "ao")
	coordinator, err := s.CreateSession(ctx, coordinatorRecord("ao", "area"))
	if err != nil {
		t.Fatal(err)
	}
	rec := sampleRecord("ao")
	rec.OwnerSessionID = &coordinator.ID
	worker, err := s.CreateSession(ctx, rec)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	link := domain.RemoteWorkerLink{
		HostID: "host-build", RemoteSessionID: "widgets-3", RemoteProjectID: "widgets",
		ProjectID: "ao", DisplayName: "remote", OwnerSessionID: &coordinator.ID, CreatedAt: now,
	}
	if err := s.UpsertRemoteWorkerLink(ctx, link); err != nil {
		t.Fatal(err)
	}

	coordinator.IsTerminated = true
	coordinator.UpdatedAt = now.Add(time.Minute)
	if err := s.UpdateSession(ctx, coordinator); err != nil {
		t.Fatal(err)
	}

	gotWorker, found, err := s.GetSession(ctx, worker.ID)
	if err != nil || !found || gotWorker.OwnerSessionID != nil {
		t.Fatalf("worker owner after termination = %v, found=%v err=%v", gotWorker.OwnerSessionID, found, err)
	}
	gotLink, found, err := s.GetRemoteWorkerLink(ctx, link.HostID, link.RemoteSessionID)
	if err != nil || !found || gotLink.OwnerSessionID != nil {
		t.Fatalf("link owner after termination = %v, found=%v err=%v", gotLink.OwnerSessionID, found, err)
	}
	localEvents, err := s.ListSessionOwnerEvents(ctx, worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if last := localEvents[len(localEvents)-1]; last.Reason != domain.OwnershipCoordinatorTerminated || last.NewOwnerSessionID != nil {
		t.Fatalf("local termination audit = %+v", localEvents)
	}
	remoteEvents, err := s.ListRemoteWorkerOwnerEvents(ctx, link.HostID, link.RemoteSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if last := remoteEvents[len(remoteEvents)-1]; last.Reason != domain.OwnershipCoordinatorTerminated || last.NewOwnerSessionID != nil {
		t.Fatalf("remote termination audit = %+v", remoteEvents)
	}
}

func TestRemoteWorkerOwnerIsFixedAtRegistration(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "ao")
	seedProject(t, s, "other")
	coordinator, err := s.CreateSession(ctx, coordinatorRecord("ao", "area"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	link := domain.RemoteWorkerLink{
		HostID: "host-build", RemoteSessionID: "widgets-3", RemoteProjectID: "widgets",
		ProjectID: "ao", DisplayName: "remote", OwnerSessionID: &coordinator.ID, CreatedAt: now,
	}
	if err := s.UpsertRemoteWorkerLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	events, err := s.ListRemoteWorkerOwnerEvents(ctx, link.HostID, link.RemoteSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Reason != domain.OwnershipSpawnParent {
		t.Fatalf("remote spawn audit = %+v", events)
	}

	moved := link
	moved.ProjectID = "other"
	moved.OwnerSessionID = nil
	moved.DisplayName = "renamed"
	if err := s.UpsertRemoteWorkerLink(ctx, moved); err != nil {
		t.Fatal(err)
	}
	got, found, err := s.GetRemoteWorkerLink(ctx, link.HostID, link.RemoteSessionID)
	if err != nil || !found {
		t.Fatalf("read link: found=%v err=%v", found, err)
	}
	if got.ProjectID != "ao" || got.OwnerSessionID == nil || *got.OwnerSessionID != coordinator.ID || got.DisplayName != "renamed" {
		t.Fatalf("re-registration changed fixed fields: %+v", got)
	}
}

func TestSetSessionOwnerGuardRefusesAClaimedReport(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "ao")
	coordinator, err := s.CreateSession(ctx, coordinatorRecord("ao", "area"))
	if err != nil {
		t.Fatal(err)
	}
	worker, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := s.CreateReport(ctx, pendingReport("rpt-guard", worker, now)); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := s.ClaimReport(ctx, "rpt-guard", "token-guard", now); err != nil || !claimed {
		t.Fatalf("claim report: claimed=%v err=%v", claimed, err)
	}

	if _, _, err := s.SetSessionOwner(ctx, worker.ID, &coordinator.ID, domain.OwnershipExplicitAssignment, now, false); !errors.Is(err, ports.ErrOwnerReassignmentInFlight) {
		t.Fatalf("guarded assignment err = %v, want ErrOwnerReassignmentInFlight", err)
	}
	got, found, err := s.GetSession(ctx, worker.ID)
	if err != nil || !found || got.OwnerSessionID != nil {
		t.Fatalf("owner after refused assignment = %v, found=%v err=%v", got.OwnerSessionID, found, err)
	}
	if events, err := s.ListSessionOwnerEvents(ctx, worker.ID); err != nil || len(events) != 0 {
		t.Fatalf("audit after refused assignment = %+v err=%v, want none", events, err)
	}

	// The unguarded path is the reconciler/automatic-release path and must
	// still work while a report is claimed.
	if _, _, err := s.SetSessionOwner(ctx, worker.ID, &coordinator.ID, domain.OwnershipOwnerNotLiveReconciled, now, true); err != nil {
		t.Fatalf("unguarded assignment: %v", err)
	}
}

func TestSetSessionOwnerGuardSerializesWithClaims(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "ao")
	coordinator, err := s.CreateSession(ctx, coordinatorRecord("ao", "area"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	const rounds = 24
	for i := 0; i < rounds; i++ {
		worker, err := s.CreateSession(ctx, sampleRecord("ao"))
		if err != nil {
			t.Fatal(err)
		}
		reportID := "rpt-race-" + string(worker.ID)
		if _, err := s.CreateReport(ctx, pendingReport(reportID, worker, now)); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		var ownerErr error
		var claimed bool
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _, ownerErr = s.SetSessionOwner(ctx, worker.ID, &coordinator.ID, domain.OwnershipExplicitAssignment, now, false)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, claimed, _ = s.ClaimReport(ctx, reportID, "token-"+string(worker.ID), now)
		}()
		close(start)
		wg.Wait()

		got, found, err := s.GetSession(ctx, worker.ID)
		if err != nil || !found {
			t.Fatalf("round %d: read worker: found=%v err=%v", i, found, err)
		}
		events, err := s.ListSessionOwnerEvents(ctx, worker.ID)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case ownerErr == nil:
			// The guarded owner write serialized before the claim, so the claim
			// must have succeeded afterwards and the audit has exactly one row.
			if !claimed || got.OwnerSessionID == nil || *got.OwnerSessionID != coordinator.ID || len(events) != 1 {
				t.Fatalf("round %d: owner write won: claimed=%v owner=%v events=%+v", i, claimed, got.OwnerSessionID, events)
			}
		case errors.Is(ownerErr, ports.ErrOwnerReassignmentInFlight):
			// The claim serialized first, so the owner write must have changed
			// nothing and written no audit row.
			if got.OwnerSessionID != nil || len(events) != 0 {
				t.Fatalf("round %d: claim won: owner=%v events=%+v", i, got.OwnerSessionID, events)
			}
		default:
			t.Fatalf("round %d: unexpected owner error: %v", i, ownerErr)
		}
	}
}

func TestCoordinatorScopeIsNormalizedAndBounded(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "ao")

	created, err := s.CreateSession(ctx, coordinatorRecord("ao", "  auth\x1b area  "))
	if err != nil {
		t.Fatalf("create coordinator with a noisy scope: %v", err)
	}
	if created.CoordinatorScope != "auth area" {
		t.Fatalf("returned scope = %q, want %q", created.CoordinatorScope, "auth area")
	}
	got, found, err := s.GetSession(ctx, created.ID)
	if err != nil || !found {
		t.Fatalf("read coordinator: found=%v err=%v", found, err)
	}
	if got.CoordinatorScope != "auth area" {
		t.Fatalf("stored scope = %q, want %q", got.CoordinatorScope, "auth area")
	}

	tooLong := coordinatorRecord("ao", strings.Repeat("x", 201))
	if _, err := s.CreateSession(ctx, tooLong); err == nil || !strings.Contains(err.Error(), "at most 200") {
		t.Fatalf("overlong scope err = %v, want the 200-character rejection", err)
	}
}

func TestPromoteTaskPreparationStoresANormalizedScope(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "ao")
	hidden := sampleRecord("ao")
	hidden.IsTaskPreparation = true
	hidden.ProvisionState = domain.SessionProvisionProvisioning
	created, err := s.CreateSession(ctx, hidden)
	if err != nil {
		t.Fatal(err)
	}
	promote := created
	promote.IsTaskPreparation = false
	promote.Kind = domain.KindCoordinator
	promote.CoordinatorScope = "  auth\x1b area  "
	promoted, err := s.PromoteTaskPreparation(ctx, created.ID, promote)
	if err != nil || !promoted {
		t.Fatalf("promote = %v err=%v", promoted, err)
	}
	got, found, err := s.GetSession(ctx, created.ID)
	if err != nil || !found {
		t.Fatalf("read promoted session: found=%v err=%v", found, err)
	}
	if got.Kind != domain.KindCoordinator || got.CoordinatorScope != "auth area" || got.OwnerSessionID != nil {
		t.Fatalf("promoted coordinator = %+v", got)
	}
}

func TestPromoteTaskPreparationWritesOneSpawnParentAudit(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "ao")
	coordinator, err := s.CreateSession(ctx, coordinatorRecord("ao", "area"))
	if err != nil {
		t.Fatal(err)
	}
	hidden := sampleRecord("ao")
	hidden.IsTaskPreparation = true
	hidden.ProvisionState = domain.SessionProvisionProvisioning
	created, err := s.CreateSession(ctx, hidden)
	if err != nil {
		t.Fatal(err)
	}
	promote := created
	promote.IsTaskPreparation = false
	promote.OwnerSessionID = &coordinator.ID
	promote.DisplayName = "promoted worker"
	promoted, err := s.PromoteTaskPreparation(ctx, created.ID, promote)
	if err != nil || !promoted {
		t.Fatalf("promote = %v err=%v", promoted, err)
	}
	got, found, err := s.GetSession(ctx, created.ID)
	if err != nil || !found {
		t.Fatalf("read promoted worker: found=%v err=%v", found, err)
	}
	if got.OwnerSessionID == nil || *got.OwnerSessionID != coordinator.ID {
		t.Fatalf("promoted owner = %v, want %s", got.OwnerSessionID, coordinator.ID)
	}
	events, err := s.ListSessionOwnerEvents(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Reason != domain.OwnershipSpawnParent ||
		events[0].PreviousOwnerSessionID != nil || events[0].NewOwnerSessionID == nil || *events[0].NewOwnerSessionID != coordinator.ID {
		t.Fatalf("promotion audit = %+v", events)
	}
	if promotedAgain, err := s.PromoteTaskPreparation(ctx, created.ID, promote); err != nil || promotedAgain {
		t.Fatalf("second promote = %v err=%v, want a no-op", promotedAgain, err)
	}
	if events, err := s.ListSessionOwnerEvents(ctx, created.ID); err != nil || len(events) != 1 {
		t.Fatalf("audit after repeat promote = %+v err=%v, want one row", events, err)
	}
}

func TestClearOwnerIfIsACompareAndSetWithoutAuditOnMismatch(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "ao")
	old, err := s.CreateSession(ctx, coordinatorRecord("ao", "old"))
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.CreateSession(ctx, coordinatorRecord("ao", "current"))
	if err != nil {
		t.Fatal(err)
	}
	worker, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	link := domain.RemoteWorkerLink{
		HostID: "host-build", RemoteSessionID: "widgets-3", RemoteProjectID: "widgets",
		ProjectID: "ao", DisplayName: "remote", OwnerSessionID: &current.ID, CreatedAt: now,
	}
	if err := s.UpsertRemoteWorkerLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SetSessionOwner(ctx, worker.ID, &current.ID, domain.OwnershipExplicitAssignment, now, false); err != nil {
		t.Fatal(err)
	}

	got, cleared, err := s.ClearSessionOwnerIf(ctx, worker.ID, old.ID, domain.OwnershipOwnerNotLiveReconciled, now)
	if err != nil || cleared || got.OwnerSessionID == nil || *got.OwnerSessionID != current.ID {
		t.Fatalf("mismatched session clear: cleared=%v owner=%v err=%v", cleared, got.OwnerSessionID, err)
	}
	gotLink, cleared, err := s.ClearRemoteWorkerOwnerIf(ctx, link.HostID, link.RemoteSessionID, old.ID, domain.OwnershipOwnerNotLiveReconciled, now)
	if err != nil || cleared || gotLink.OwnerSessionID == nil || *gotLink.OwnerSessionID != current.ID {
		t.Fatalf("mismatched link clear: cleared=%v owner=%v err=%v", cleared, gotLink.OwnerSessionID, err)
	}
	events, err := s.ListSessionOwnerEvents(ctx, worker.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("session audit after mismatch = %+v err=%v, want only the assignment", events, err)
	}
	remoteEvents, err := s.ListRemoteWorkerOwnerEvents(ctx, link.HostID, link.RemoteSessionID)
	if err != nil || len(remoteEvents) != 1 {
		t.Fatalf("link audit after mismatch = %+v err=%v, want only the spawn row", remoteEvents, err)
	}

	got, cleared, err = s.ClearSessionOwnerIf(ctx, worker.ID, current.ID, domain.OwnershipOwnerNotLiveReconciled, now)
	if err != nil || !cleared || got.OwnerSessionID != nil {
		t.Fatalf("matching session clear: cleared=%v owner=%v err=%v", cleared, got.OwnerSessionID, err)
	}
	gotLink, cleared, err = s.ClearRemoteWorkerOwnerIf(ctx, link.HostID, link.RemoteSessionID, current.ID, domain.OwnershipOwnerNotLiveReconciled, now)
	if err != nil || !cleared || gotLink.OwnerSessionID != nil {
		t.Fatalf("matching link clear: cleared=%v owner=%v err=%v", cleared, gotLink.OwnerSessionID, err)
	}
	events, _ = s.ListSessionOwnerEvents(ctx, worker.ID)
	remoteEvents, _ = s.ListRemoteWorkerOwnerEvents(ctx, link.HostID, link.RemoteSessionID)
	if len(events) != 2 || events[1].Reason != domain.OwnershipOwnerNotLiveReconciled || len(remoteEvents) != 2 || remoteEvents[1].Reason != domain.OwnershipOwnerNotLiveReconciled {
		t.Fatalf("audit after clears: session=%+v link=%+v", events, remoteEvents)
	}
	if _, cleared, err = s.ClearSessionOwnerIf(ctx, worker.ID, current.ID, domain.OwnershipOwnerNotLiveReconciled, now); err != nil || cleared {
		t.Fatalf("repeat clear: cleared=%v err=%v", cleared, err)
	}
}

func TestCoordinatorWorkerLimitIsStoredUpdatedAndAudited(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	seedProject(t, s, "ao")
	two := 2
	rec := coordinatorRecord("ao", "auth area")
	rec.CoordinatorWorkerLimit = &two
	limited, err := s.CreateSession(ctx, rec)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := s.CreateSession(ctx, coordinatorRecord("ao", "docs"))
	if err != nil {
		t.Fatal(err)
	}
	if limited.CoordinatorWorkerLimit == nil || *limited.CoordinatorWorkerLimit != 2 || plain.CoordinatorWorkerLimit != nil {
		t.Fatalf("created limits = %v / %v, want 2 / nil", limited.CoordinatorWorkerLimit, plain.CoordinatorWorkerLimit)
	}

	now := time.Now().UTC().Truncate(time.Second)
	one, three := 1, 3
	if err := s.SetCoordinatorLimits(ctx, limited.ID, &one, nil, domain.CoordinatorLimitEvent{ActorSessionID: "ao-root", Origin: "ui"}, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCoordinatorLimits(ctx, plain.ID, &three, nil, domain.CoordinatorLimitEvent{ActorSessionID: "ao-root", Origin: "root"}, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	got, ok, err := reopened.GetSession(ctx, limited.ID)
	if err != nil || !ok || got.CoordinatorWorkerLimit == nil || *got.CoordinatorWorkerLimit != 1 {
		t.Fatalf("limit after restart = %v ok=%v err=%v, want 1", got.CoordinatorWorkerLimit, ok, err)
	}
	events, err := reopened.ListCoordinatorLimitEvents(ctx, limited.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("events = %+v err=%v", events, err)
	}
	if e := events[0]; e.NewLimit == nil || *e.NewLimit != 1 || e.PreviousLimit == nil || *e.PreviousLimit != 2 || e.ActorSessionID != "ao-root" || e.Origin != "ui" {
		t.Fatalf("audit row = %+v", e)
	}
}

func TestSetCoordinatorWorkerLimitRefusesNonCoordinatorsAndTerminatedOnes(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	seedProject(t, s, "ao")
	worker, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	event := domain.CoordinatorLimitEvent{ActorSessionID: "ao-root", Origin: "root"}
	one := 1
	if err := s.SetCoordinatorLimits(ctx, worker.ID, &one, nil, event, now); err == nil {
		t.Fatal("a worker has no coordinator limit")
	}
	if err := s.SetCoordinatorLimits(ctx, "ao-ghost", &one, nil, event, now); !errors.Is(err, ports.ErrSessionNotFound) {
		t.Fatalf("unknown session err = %v", err)
	}
	if events, _ := s.ListCoordinatorLimitEvents(ctx, worker.ID); len(events) != 0 {
		t.Fatalf("a refused write left audit rows: %+v", events)
	}
}

func TestCoordinatorHostSlotsPersistAndAreAudited(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertProject(ctx, domain.ProjectRecord{ID: "ao", Path: "/tmp/ao", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	rec := coordinatorRecord("ao", "auth area")
	rec.CoordinatorHostSlots = []domain.HostSlot{{HostID: "host-a", Slots: 1}}
	created, err := s.CreateSession(ctx, rec)
	if err != nil {
		t.Fatal(err)
	}
	if len(created.CoordinatorHostSlots) != 1 {
		t.Fatalf("slots at spawn = %+v, want 1", created.CoordinatorHostSlots)
	}
	now := time.Now().UTC().Truncate(time.Second)
	next := []domain.HostSlot{{HostID: "host-a", Slots: 2}, {HostID: "host-b", Slots: 1}}
	if err := s.SetCoordinatorLimits(ctx, created.ID, nil, next, domain.CoordinatorLimitEvent{ActorSessionID: "ao-root", Origin: "cli"}, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCoordinatorLimits(ctx, created.ID, nil, nil, domain.CoordinatorLimitEvent{ActorSessionID: "ao-root", Origin: "ui"}, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	got, _, err := reopened.GetSession(ctx, created.ID)
	if err != nil || len(got.CoordinatorHostSlots) != 0 {
		t.Fatalf("slots after clearing = %+v err=%v, want none", got.CoordinatorHostSlots, err)
	}
	listed, err := reopened.ListSessions(ctx, "ao")
	if err != nil || len(listed) != 1 {
		t.Fatalf("list = %v err=%v", listed, err)
	}
	events, err := reopened.ListCoordinatorLimitEvents(ctx, created.ID)
	if err != nil || len(events) != 2 {
		t.Fatalf("events = %+v err=%v", events, err)
	}
	first, second := events[0], events[1]
	if len(first.PreviousHostSlots) != 1 || len(first.NewHostSlots) != 2 || first.NewLimit != nil || first.PreviousLimit != nil {
		t.Fatalf("first audit row = %+v, want slots 1 -> 2 and untouched NULL limits", first)
	}
	if len(second.PreviousHostSlots) != 2 || len(second.NewHostSlots) != 0 || second.Origin != "ui" {
		t.Fatalf("second audit row = %+v, want slots 2 -> none", second)
	}
}
