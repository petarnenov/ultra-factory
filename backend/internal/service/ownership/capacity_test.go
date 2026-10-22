package ownership

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type listedSessions []domain.SessionRecord

func (l listedSessions) ListSessions(context.Context, domain.ProjectID) ([]domain.SessionRecord, error) {
	return l, nil
}

func intp(n int) *int { return &n }

func TestOwnerLimitResolution(t *testing.T) {
	limits := domain.CoordinatorLimits{Coordinators: 2, WorkersPerCoordinator: 3, Workers: 6}
	sessions := listedSessions{
		{ID: "c-own", Kind: domain.KindCoordinator, CoordinatorWorkerLimit: intp(2)},
		{ID: "c-default", Kind: domain.KindCoordinator},
		{ID: "c-above", Kind: domain.KindCoordinator, CoordinatorWorkerLimit: intp(5)},
		{ID: "c-dead", Kind: domain.KindCoordinator, IsTerminated: true, CoordinatorWorkerLimit: intp(1)},
	}
	counts, err := Count(context.Background(), sessions, nil, "ao")
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[domain.SessionID]int{
		"c-own": 2, "c-default": 3, "c-above": 3, "c-dead": 1, "unknown": 3,
	} {
		if got := counts.OwnerLimit(id, limits); got != want {
			t.Errorf("%s limit = %d, want %d", id, got, want)
		}
	}
	raised := domain.CoordinatorLimits{Coordinators: 2, WorkersPerCoordinator: 6, Workers: 12}
	if got := counts.OwnerLimit("c-above", raised); got != 5 {
		t.Errorf("own limit below the project limit = %d, want 5", got)
	}
}

func TestCheckOwnerUsesTheCoordinatorsOwnLimit(t *testing.T) {
	limits := domain.CoordinatorLimits{Coordinators: 2, WorkersPerCoordinator: 3, Workers: 6}
	owner := domain.SessionID("c1")
	counts := Counts{ByOwner: map[domain.SessionID]int{owner: 2}, OwnerLimits: map[domain.SessionID]int{owner: 2}}
	err := counts.CheckOwner(&owner, limits)
	if err == nil {
		t.Fatal("a coordinator with limit 2 and 2 workers must refuse a third")
	}
	counts.OwnerLimits = nil
	if err := counts.CheckOwner(&owner, limits); err != nil {
		t.Fatalf("without an own limit the project's 3 applies: %v", err)
	}
	if err := counts.CheckOwner(nil, limits); err != nil {
		t.Fatalf("a root-owned worker has no owner cap: %v", err)
	}
}

func TestLoweredLimitKeepsWorkersButBlocksNewOnes(t *testing.T) {
	limits := domain.CoordinatorLimits{Coordinators: 2, WorkersPerCoordinator: 3, Workers: 6}
	owner := domain.SessionID("c1")
	counts := Counts{Coordinators: 1, Workers: 2, ByOwner: map[domain.SessionID]int{owner: 2}, OwnerLimits: map[domain.SessionID]int{owner: 1}}
	if err := counts.CheckNewWorker("ao", &owner, limits); err == nil {
		t.Fatal("2 workers over a limit of 1 must block a new one")
	}
	counts.ByOwner[owner] = 1
	if err := counts.CheckNewWorker("ao", &owner, limits); err == nil {
		t.Fatal("1 worker at a limit of 1 must still block")
	}
	counts.ByOwner[owner] = 0
	if err := counts.CheckNewWorker("ao", &owner, limits); err != nil {
		t.Fatalf("a free slot admits one worker: %v", err)
	}
}

type listedLinks []domain.RemoteWorkerLink

func (l listedLinks) ListRemoteWorkerLinks(context.Context) ([]domain.RemoteWorkerLink, error) {
	return l, nil
}

// TestStaleLinksDoNotCountTowardCapacity keeps a link in the count only while
// its host answered within the grace period; a stale link is reported apart.
func TestStaleLinksDoNotCountTowardCapacity(t *testing.T) {
	now := time.Now()
	owner := domain.SessionID("coord-1")
	fresh := now.Add(-time.Minute)
	old := now.Add(-2 * domain.RemoteWorkerLinkStaleAfter)
	live := domain.RemoteWorkerLink{HostID: "h", RemoteSessionID: "w-live", ProjectID: "p", OwnerSessionID: &owner, CreatedAt: now, LastSeenAt: &fresh, LastState: domain.RemoteLinkLive}
	stale := domain.RemoteWorkerLink{HostID: "h", RemoteSessionID: "w-stale", ProjectID: "p", OwnerSessionID: &owner, CreatedAt: old, LastSeenAt: &old, LastState: domain.RemoteLinkLive}
	ended := domain.RemoteWorkerLink{HostID: "h", RemoteSessionID: "w-ended", ProjectID: "p", OwnerSessionID: &owner, CreatedAt: now, LastSeenAt: &fresh, LastState: domain.RemoteLinkTerminated}
	counts, err := Count(context.Background(), listedSessions{}, listedLinks{live, stale, ended}, "p")
	if err != nil {
		t.Fatal(err)
	}
	if counts.Workers != 1 || counts.ByOwner[owner] != 1 || counts.StaleWorkers != 1 {
		t.Fatalf("workers=%d byOwner=%d stale=%d, want 1 live and 1 stale", counts.Workers, counts.ByOwner[owner], counts.StaleWorkers)
	}
}
