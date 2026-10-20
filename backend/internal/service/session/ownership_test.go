package session

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func ownershipStore() (*fakeStore, domain.SessionRecord, domain.SessionRecord) {
	st := newFakeStore()
	st.projects["ao"] = domain.ProjectRecord{ID: "ao", Config: domain.ProjectConfig{Coordinators: domain.CoordinatorsConfig{AllowLocalWorkers: true}}}
	root := domain.SessionRecord{ID: "ao-1", ProjectID: "ao", Kind: domain.KindOrchestrator}
	st.sessions[root.ID] = root
	coordinator := domain.SessionRecord{ID: "ao-2", ProjectID: "ao", Kind: domain.KindCoordinator, CoordinatorScope: "auth"}
	st.sessions[coordinator.ID] = coordinator
	return st, root, coordinator
}

func TestSpawnDerivesOwnerOnlyFromALiveCoordinatorParent(t *testing.T) {
	liveCoordinator := domain.SessionRecord{ID: "ao-1", ProjectID: "ao", Kind: domain.KindCoordinator}
	terminatedCoordinator := domain.SessionRecord{ID: "ao-2", ProjectID: "ao", Kind: domain.KindCoordinator, IsTerminated: true}
	root := domain.SessionRecord{ID: "ao-3", ProjectID: "ao", Kind: domain.KindOrchestrator}
	worker := domain.SessionRecord{ID: "ao-4", ProjectID: "ao", Kind: domain.KindWorker}
	otherProject := domain.SessionRecord{ID: "other-1", ProjectID: "other", Kind: domain.KindCoordinator}

	cases := []struct {
		name   string
		parent domain.SessionRecord
		want   domain.SessionID
	}{
		{"live coordinator owns", liveCoordinator, "ao-1"},
		{"terminated coordinator is root-owned", terminatedCoordinator, ""},
		{"root parent is root-owned", root, ""},
		{"worker parent is root-owned", worker, ""},
		{"cross-project coordinator is root-owned", otherProject, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newFakeStore()
			st.projects["ao"] = domain.ProjectRecord{ID: "ao", Config: domain.ProjectConfig{Coordinators: domain.CoordinatorsConfig{AllowLocalWorkers: true}}}
			st.sessions[tc.parent.ID] = tc.parent
			fc := &fakeCommander{spawnRecord: domain.SessionRecord{ID: "ao-9", ProjectID: "ao", Kind: domain.KindWorker}}
			svc := &Service{manager: fc, store: st}

			_, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{
				ProjectID: "ao", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode,
				ParentSessionID: tc.parent.ID,
			})
			if err != nil {
				t.Fatal(err)
			}
			got := fc.spawnedCfg.OwnerSessionID
			if tc.want == "" {
				if got != nil {
					t.Fatalf("owner = %v, want root-owned", *got)
				}
				return
			}
			if got == nil || *got != tc.want {
				t.Fatalf("owner = %v, want %s", got, tc.want)
			}
		})
	}
}

func TestSpawnWithoutParentLeavesTheWorkerRootOwned(t *testing.T) {
	st := newFakeStore()
	st.projects["ao"] = domain.ProjectRecord{ID: "ao", Config: domain.ProjectConfig{Coordinators: domain.CoordinatorsConfig{AllowLocalWorkers: true}}}
	fc := &fakeCommander{spawnRecord: domain.SessionRecord{ID: "ao-9", ProjectID: "ao", Kind: domain.KindWorker}}
	svc := &Service{manager: fc, store: st}

	if _, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{
		ProjectID: "ao", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode,
	}); err != nil {
		t.Fatal(err)
	}
	if fc.spawnedCfg.OwnerSessionID != nil {
		t.Fatalf("owner = %v, want root-owned", *fc.spawnedCfg.OwnerSessionID)
	}
}

func TestAssignOwnerAssignsReassignsAndReturnsToRoot(t *testing.T) {
	st, root, coordinator := ownershipStore()
	other := domain.SessionRecord{ID: "ao-3", ProjectID: "ao", Kind: domain.KindCoordinator, CoordinatorScope: "docs"}
	st.sessions[other.ID] = other
	worker := domain.SessionRecord{ID: "ao-4", ProjectID: "ao", Kind: domain.KindWorker}
	st.sessions[worker.ID] = worker
	svc := &Service{store: st}
	ctx := context.Background()

	assigned, err := svc.AssignOwner(ctx, worker.ID, &coordinator.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if assigned.OwnerSessionID == nil || *assigned.OwnerSessionID != coordinator.ID {
		t.Fatalf("assigned owner = %v", assigned.OwnerSessionID)
	}
	reassigned, err := svc.AssignOwner(ctx, worker.ID, &other.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reassigned.OwnerSessionID == nil || *reassigned.OwnerSessionID != other.ID {
		t.Fatalf("reassigned owner = %v", reassigned.OwnerSessionID)
	}
	rooted, err := svc.AssignOwner(ctx, worker.ID, nil, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rooted.OwnerSessionID != nil {
		t.Fatalf("root-assigned owner = %v", rooted.OwnerSessionID)
	}
	if len(st.ownerEvents) != 3 {
		t.Fatalf("owner audit = %+v", st.ownerEvents)
	}
	if st.ownerEvents[0].Reason != domain.OwnershipExplicitAssignment ||
		st.ownerEvents[1].Reason != domain.OwnershipExplicitReassignment ||
		st.ownerEvents[2].Reason != domain.OwnershipExplicitReassignment {
		t.Fatalf("owner audit reasons = %+v", st.ownerEvents)
	}
}

func TestAssignOwnerIsIdempotent(t *testing.T) {
	st, root, coordinator := ownershipStore()
	worker := domain.SessionRecord{ID: "ao-3", ProjectID: "ao", Kind: domain.KindWorker}
	st.sessions[worker.ID] = worker
	svc := &Service{store: st}
	ctx := context.Background()

	if _, err := svc.AssignOwner(ctx, worker.ID, &coordinator.ID, root.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AssignOwner(ctx, worker.ID, &coordinator.ID, root.ID); err != nil {
		t.Fatal(err)
	}
	if len(st.ownerEvents) != 1 {
		t.Fatalf("repeat assignment wrote %d audit rows, want 1", len(st.ownerEvents))
	}
}

func TestAssignOwnerRefusals(t *testing.T) {
	st, root, coordinator := ownershipStore()
	other := domain.SessionRecord{ID: "other-1", ProjectID: "other", Kind: domain.KindCoordinator}
	st.sessions[other.ID] = other
	terminated := domain.SessionRecord{ID: "ao-3", ProjectID: "ao", Kind: domain.KindCoordinator, IsTerminated: true}
	st.sessions[terminated.ID] = terminated
	worker := domain.SessionRecord{ID: "ao-4", ProjectID: "ao", Kind: domain.KindWorker}
	st.sessions[worker.ID] = worker
	standalone := domain.SessionRecord{ID: "standalone-1", Kind: domain.KindWorker}
	st.sessions[standalone.ID] = standalone
	cases := []struct {
		name        string
		worker      domain.SessionID
		owner       *domain.SessionID
		requestedBy domain.SessionID
		claimed     bool
		code        string
	}{
		{"unknown worker", "ghost", nil, root.ID, false, "SESSION_NOT_FOUND"},
		{"non-worker target", coordinator.ID, nil, root.ID, false, "OWNER_TARGET_NOT_WORKER"},
		{"standalone target", standalone.ID, nil, root.ID, false, "OWNERSHIP_PROJECT_REQUIRED"},
		{"unknown caller", worker.ID, nil, "ghost", false, "SESSION_NOT_FOUND"},
		{"helper may not assign", worker.ID, nil, coordinator.ID, false, "OWNERSHIP_ROOT_ONLY"},
		{"cross-project owner", worker.ID, &other.ID, root.ID, false, "OWNER_CROSS_PROJECT"},
		{"unknown owner", worker.ID, ptrToSessionID("ghost-owner"), root.ID, false, "SESSION_NOT_FOUND"},
		{"root is not a coordinator", worker.ID, &root.ID, root.ID, false, "OWNER_NOT_COORDINATOR"},
		{"terminated owner", worker.ID, &terminated.ID, root.ID, false, "OWNER_TERMINATED"},
		{"claim in flight", worker.ID, &coordinator.ID, root.ID, true, "OWNER_REASSIGNMENT_IN_FLIGHT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st.claimedReports[worker.ID] = tc.claimed
			_, err := (&Service{store: st}).AssignOwner(context.Background(), tc.worker, tc.owner, tc.requestedBy)
			var apiErr *apierr.Error
			if !errors.As(err, &apiErr) || apiErr.Code != tc.code {
				t.Fatalf("err = %v, want %s", err, tc.code)
			}
		})
	}
}

func ptrToSessionID(id domain.SessionID) *domain.SessionID { return &id }

func TestAssignOwnerAllowsReassignmentOfATerminatedWorker(t *testing.T) {
	st, root, coordinator := ownershipStore()
	worker := domain.SessionRecord{ID: "ao-3", ProjectID: "ao", Kind: domain.KindWorker, IsTerminated: true}
	st.sessions[worker.ID] = worker
	assigned, err := (&Service{store: st}).AssignOwner(context.Background(), worker.ID, &coordinator.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if assigned.OwnerSessionID == nil || *assigned.OwnerSessionID != coordinator.ID {
		t.Fatalf("owner = %v", assigned.OwnerSessionID)
	}
}

func TestOwnershipReadsResolveOwnerScopeAndLiveRoot(t *testing.T) {
	st := newFakeStore()
	st.sessions["ao-1"] = domain.SessionRecord{ID: "ao-1", ProjectID: "ao", Kind: domain.KindOrchestrator}
	st.sessions["ao-2"] = domain.SessionRecord{ID: "ao-2", ProjectID: "ao", Kind: domain.KindCoordinator, CoordinatorScope: "auth"}
	st.sessions["ao-3"] = domain.SessionRecord{ID: "ao-3", ProjectID: "ao", Kind: domain.KindWorker, OwnerSessionID: ptrToSessionID("ao-2")}
	st.sessions["ao-4"] = domain.SessionRecord{ID: "ao-4", ProjectID: "ao", Kind: domain.KindWorker}
	svc := &Service{store: st}
	ctx := context.Background()

	owned, err := svc.Ownership(ctx, "ao-3")
	if err != nil {
		t.Fatal(err)
	}
	if owned.Owner == nil || owned.Owner.ID != "ao-2" || owned.Owner.CoordinatorScope != "auth" {
		t.Fatalf("owner = %+v, want coordinator ao-2 with scope auth", owned.Owner)
	}
	if owned.RootSessionID == nil || *owned.RootSessionID != "ao-1" {
		t.Fatalf("root = %v, want ao-1", owned.RootSessionID)
	}

	rootOwned, err := svc.Ownership(ctx, "ao-4")
	if err != nil {
		t.Fatal(err)
	}
	if rootOwned.Owner != nil {
		t.Fatalf("root-owned worker owner = %s, want nil", rootOwned.Owner.ID)
	}
	if rootOwned.RootSessionID == nil || *rootOwned.RootSessionID != "ao-1" {
		t.Fatalf("root-owned worker root = %v, want ao-1", rootOwned.RootSessionID)
	}

	coordinator, err := svc.Ownership(ctx, "ao-2")
	if err != nil {
		t.Fatal(err)
	}
	if coordinator.Owner != nil || coordinator.Session.CoordinatorScope != "auth" {
		t.Fatalf("coordinator ownership = %+v", coordinator)
	}

	root, err := svc.Ownership(ctx, "ao-1")
	if err != nil {
		t.Fatal(err)
	}
	if root.RootSessionID == nil || *root.RootSessionID != "ao-1" {
		t.Fatalf("root root = %v, want itself", root.RootSessionID)
	}
}

func TestCoordinatorRosterGroupsOwnedWorkersAndHidesTerminatedByDefault(t *testing.T) {
	st := newFakeStore()
	st.projects["ao"] = domain.ProjectRecord{ID: "ao", Config: domain.ProjectConfig{Coordinators: domain.CoordinatorsConfig{AllowLocalWorkers: true}}}
	st.sessions["ao-1"] = domain.SessionRecord{ID: "ao-1", ProjectID: "ao", Kind: domain.KindOrchestrator}
	st.sessions["ao-2"] = domain.SessionRecord{ID: "ao-2", ProjectID: "ao", Kind: domain.KindCoordinator, CoordinatorScope: "auth"}
	st.sessions["ao-3"] = domain.SessionRecord{ID: "ao-3", ProjectID: "ao", Kind: domain.KindCoordinator, CoordinatorScope: "docs"}
	st.sessions["ao-4"] = domain.SessionRecord{ID: "ao-4", ProjectID: "ao", Kind: domain.KindWorker, OwnerSessionID: ptrToSessionID("ao-2")}
	st.sessions["ao-5"] = domain.SessionRecord{ID: "ao-5", ProjectID: "ao", Kind: domain.KindWorker, OwnerSessionID: ptrToSessionID("ao-2")}
	st.sessions["ao-6"] = domain.SessionRecord{ID: "ao-6", ProjectID: "ao", Kind: domain.KindWorker, OwnerSessionID: ptrToSessionID("ao-3")}
	st.sessions["ao-7"] = domain.SessionRecord{ID: "ao-7", ProjectID: "ao", Kind: domain.KindWorker}
	st.sessions["ao-8"] = domain.SessionRecord{ID: "ao-8", ProjectID: "ao", Kind: domain.KindCoordinator, CoordinatorScope: "old", IsTerminated: true}
	st.sessions["ao-9"] = domain.SessionRecord{ID: "ao-9", ProjectID: "ao", Kind: domain.KindWorker, IsTerminated: true}
	svc := &Service{store: st}

	roster, err := svc.CoordinatorRoster(context.Background(), "ao", false)
	if err != nil {
		t.Fatal(err)
	}
	if roster.RootSessionID == nil || *roster.RootSessionID != "ao-1" {
		t.Fatalf("root = %v, want ao-1", roster.RootSessionID)
	}
	if len(roster.Coordinators) != 2 {
		t.Fatalf("coordinators = %+v, want 2 live", roster.Coordinators)
	}
	if len(roster.Coordinators[0].Workers) != 2 || roster.Coordinators[0].Workers[0].ID != "ao-4" {
		t.Fatalf("coordinator ao-2 workers = %+v", roster.Coordinators[0].Workers)
	}
	if len(roster.Coordinators[1].Workers) != 1 || roster.Coordinators[1].Workers[0].ID != "ao-6" {
		t.Fatalf("coordinator ao-3 workers = %+v", roster.Coordinators[1].Workers)
	}
	if len(roster.RootWorkers) != 1 || roster.RootWorkers[0].ID != "ao-7" {
		t.Fatalf("root workers = %+v", roster.RootWorkers)
	}

	all, err := svc.CoordinatorRoster(context.Background(), "ao", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Coordinators) != 3 || !all.Coordinators[2].Coordinator.IsTerminated {
		t.Fatalf("coordinators with history = %+v", all.Coordinators)
	}
	if len(all.RootWorkers) != 2 || all.RootWorkers[1].ID != "ao-9" {
		t.Fatalf("root workers with history = %+v", all.RootWorkers)
	}
}

func TestCoordinatorRosterRejectsAnUnknownProject(t *testing.T) {
	_, err := (&Service{store: newFakeStore()}).CoordinatorRoster(context.Background(), "ghost", false)
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "PROJECT_NOT_FOUND" {
		t.Fatalf("err = %v, want PROJECT_NOT_FOUND", err)
	}
}

func TestCoordinatorRosterFlagsATerminatedCoordinatorWithLiveWorkers(t *testing.T) {
	st := newFakeStore()
	st.projects["ao"] = domain.ProjectRecord{ID: "ao", Config: domain.ProjectConfig{Coordinators: domain.CoordinatorsConfig{AllowLocalWorkers: true}}}
	st.sessions["ao-1"] = domain.SessionRecord{ID: "ao-1", ProjectID: "ao", Kind: domain.KindOrchestrator}
	st.sessions["ao-2"] = domain.SessionRecord{ID: "ao-2", ProjectID: "ao", Kind: domain.KindCoordinator, CoordinatorScope: "stranded", IsTerminated: true}
	st.sessions["ao-3"] = domain.SessionRecord{ID: "ao-3", ProjectID: "ao", Kind: domain.KindWorker, OwnerSessionID: ptrToSessionID("ao-2")}
	st.sessions["ao-4"] = domain.SessionRecord{ID: "ao-4", ProjectID: "ao", Kind: domain.KindCoordinator, CoordinatorScope: "done", IsTerminated: true}
	st.sessions["ao-5"] = domain.SessionRecord{ID: "ao-5", ProjectID: "ao", Kind: domain.KindWorker, OwnerSessionID: ptrToSessionID("ao-4"), IsTerminated: true}
	svc := &Service{store: st}

	roster, err := svc.CoordinatorRoster(context.Background(), "ao", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(roster.Coordinators) != 1 || roster.Coordinators[0].Coordinator.ID != "ao-2" ||
		!roster.Coordinators[0].NeedsRecovery || len(roster.Coordinators[0].Workers) != 1 {
		t.Fatalf("default roster must list only the stranded coordinator: %+v", roster.Coordinators)
	}

	all, err := svc.CoordinatorRoster(context.Background(), "ao", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Coordinators) != 2 || !all.Coordinators[0].NeedsRecovery || all.Coordinators[1].NeedsRecovery {
		t.Fatalf("a terminated coordinator with only terminated workers needs no recovery: %+v", all.Coordinators)
	}
}

func TestSpawnAutoReviewDefaultsOnForCoordinatorOwnedWorkers(t *testing.T) {
	off := false
	cases := []struct {
		name     string
		parent   domain.SessionRecord
		explicit *bool
		want     *bool
	}{
		{"coordinator-owned defaults on", domain.SessionRecord{ID: "ao-1", ProjectID: "ao", Kind: domain.KindCoordinator}, nil, boolRef(true)},
		{"explicit false wins", domain.SessionRecord{ID: "ao-1", ProjectID: "ao", Kind: domain.KindCoordinator}, &off, &off},
		{"root-owned is unchanged", domain.SessionRecord{ID: "ao-1", ProjectID: "ao", Kind: domain.KindOrchestrator}, nil, nil},
		{"terminated coordinator is unchanged", domain.SessionRecord{ID: "ao-1", ProjectID: "ao", Kind: domain.KindCoordinator, IsTerminated: true}, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newFakeStore()
			st.projects["ao"] = domain.ProjectRecord{ID: "ao", Config: domain.ProjectConfig{Coordinators: domain.CoordinatorsConfig{AllowLocalWorkers: true}}}
			st.sessions[tc.parent.ID] = tc.parent
			fc := &fakeCommander{spawnRecord: domain.SessionRecord{ID: "ao-9", ProjectID: "ao", Kind: domain.KindWorker}}
			svc := &Service{manager: fc, store: st}
			if _, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{
				ProjectID: "ao", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode,
				ParentSessionID: tc.parent.ID, AutoReview: tc.explicit,
			}); err != nil {
				t.Fatal(err)
			}
			got := fc.spawnedCfg.AutoReview
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Fatalf("autoReview = %v, want %v", got, tc.want)
			}
		})
	}
}

func boolRef(v bool) *bool { return &v }
