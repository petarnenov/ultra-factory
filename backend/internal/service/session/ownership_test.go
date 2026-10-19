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
			st.projects["ao"] = domain.ProjectRecord{ID: "ao"}
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
	st.projects["ao"] = domain.ProjectRecord{ID: "ao"}
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
