package remoteworker

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
	coordownership "github.com/aoagents/agent-orchestrator/backend/internal/service/ownership"
)

const capProject = domain.ProjectID("ultra-factory")

func capSessions(extra ...domain.SessionRecord) map[domain.SessionID]domain.SessionRecord {
	out := map[domain.SessionID]domain.SessionRecord{
		"ao-1": {ID: "ao-1", ProjectID: capProject, Kind: domain.KindOrchestrator},
		"c1":   {ID: "c1", ProjectID: capProject, Kind: domain.KindCoordinator},
		"c2":   {ID: "c2", ProjectID: capProject, Kind: domain.KindCoordinator},
	}
	for _, rec := range extra {
		out[rec.ID] = rec
	}
	return out
}

func capService(store *fakeStore, guard *coordownership.Guard) *Service {
	return New(Deps{Store: store, Capacity: guard, Lookup: savedHosts(remotehost.Entry{Label: "b", URL: "https://b", HostID: "host-build"})})
}

func ownedLinks(owner domain.SessionID, n int, offset int) []domain.RemoteWorkerLink {
	var out []domain.RemoteWorkerLink
	for i := 0; i < n; i++ {
		id := owner
		out = append(out, domain.RemoteWorkerLink{
			HostID: "host-build", RemoteSessionID: domain.SessionID(fmt.Sprintf("w-%d", offset+i)),
			RemoteProjectID: "widgets", ProjectID: capProject, OwnerSessionID: &id,
		})
	}
	return out
}

func TestRegisterEnforcesTheCoordinatorCapForNewLinks(t *testing.T) {
	store := &fakeStore{sessions: capSessions(), stored: ownedLinks("c1", 3, 0)}
	in := validInput()
	in.ParentSessionID = "c1"
	_, err := capService(store, nil).Register(context.Background(), in)
	if codeOf(err) != "COORDINATOR_WORKER_LIMIT_REACHED" {
		t.Fatalf("err = %v, want COORDINATOR_WORKER_LIMIT_REACHED", err)
	}
	if len(store.stored) != 3 {
		t.Fatalf("a rejected registration stored a link: %d", len(store.stored))
	}
	in.ParentSessionID = "c2"
	if _, err := capService(store, nil).Register(context.Background(), in); err != nil {
		t.Fatalf("another coordinator has room: %v", err)
	}
}

func TestRegisterEnforcesTheProjectTotalAndCountsLocalWorkers(t *testing.T) {
	var locals []domain.SessionRecord
	for i := 0; i < 4; i++ {
		locals = append(locals, domain.SessionRecord{ID: domain.SessionID(fmt.Sprintf("w%d", i)), ProjectID: capProject, Kind: domain.KindWorker})
	}
	store := &fakeStore{sessions: capSessions(locals...), stored: ownedLinks("c1", 2, 0)}
	_, err := capService(store, nil).Register(context.Background(), validInput())
	if codeOf(err) != "PROJECT_WORKER_LIMIT_REACHED" {
		t.Fatalf("err = %v, want PROJECT_WORKER_LIMIT_REACHED", err)
	}
}

func TestRegisterWithoutCoordinatorsIsNotCapped(t *testing.T) {
	var locals []domain.SessionRecord
	for i := 0; i < 8; i++ {
		locals = append(locals, domain.SessionRecord{ID: domain.SessionID(fmt.Sprintf("w%d", i)), ProjectID: capProject, Kind: domain.KindWorker})
	}
	store := &fakeStore{sessions: map[domain.SessionID]domain.SessionRecord{
		"ao-1": {ID: "ao-1", ProjectID: capProject, Kind: domain.KindOrchestrator},
	}}
	for _, rec := range locals {
		store.sessions[rec.ID] = rec
	}
	if _, err := capService(store, nil).Register(context.Background(), validInput()); err != nil {
		t.Fatalf("a project without coordinators keeps unlimited links: %v", err)
	}
}

func TestReRegisteringAnExistingLinkNeedsNoSlot(t *testing.T) {
	store := &fakeStore{sessions: capSessions(), stored: append(ownedLinks("c1", 2, 0), domain.RemoteWorkerLink{
		HostID: "host-build", RemoteSessionID: "widgets-3", RemoteProjectID: "widgets", ProjectID: capProject,
		OwnerSessionID: ownerPtr("c1"),
	})}
	in := validInput()
	in.ParentSessionID = "c1"
	if _, err := capService(store, nil).Register(context.Background(), in); err != nil {
		t.Fatalf("re-registration of a counted link: %v", err)
	}
}

func ownerPtr(id domain.SessionID) *domain.SessionID { return &id }

func TestAssignOwnerEnforcesTheCoordinatorCapOnRemoteLinks(t *testing.T) {
	free := domain.RemoteWorkerLink{HostID: "host-build", RemoteSessionID: "free", RemoteProjectID: "widgets", ProjectID: capProject}
	store := &fakeStore{sessions: capSessions(domain.SessionRecord{ID: "w1", ProjectID: capProject, Kind: domain.KindWorker, OwnerSessionID: ownerPtr("c1")}),
		stored: append(ownedLinks("c1", 2, 0), free)}
	svc := capService(store, nil)
	ctx := context.Background()

	if _, err := svc.AssignOwner(ctx, "host-build", "free", ownerPtr("c1"), "ao-1"); codeOf(err) != "COORDINATOR_WORKER_LIMIT_REACHED" {
		t.Fatalf("err = %v, want COORDINATOR_WORKER_LIMIT_REACHED (2 links + 1 local worker)", err)
	}
	if _, err := svc.AssignOwner(ctx, "host-build", "w-0", ownerPtr("c1"), "ao-1"); err != nil {
		t.Fatalf("re-assigning to the current owner is a no-op: %v", err)
	}
	if _, err := svc.AssignOwner(ctx, "host-build", "free", ownerPtr("c2"), "ao-1"); err != nil {
		t.Fatalf("another coordinator has room: %v", err)
	}
	if _, err := svc.AssignOwner(ctx, "host-build", "w-0", nil, "ao-1"); err != nil {
		t.Fatalf("moving to the root is never capacity checked: %v", err)
	}
}

func TestRegisterWaitsForTheSharedProjectLock(t *testing.T) {
	guard := &coordownership.Guard{}
	store := &fakeStore{sessions: capSessions()}
	svc := capService(store, guard)
	lock := guard.Project(capProject)
	lock.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := svc.Register(context.Background(), validInput())
		done <- err
	}()
	select {
	case <-done:
		t.Fatal("Register must wait for a spawn holding the project lock")
	case <-time.After(50 * time.Millisecond):
	}
	lock.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentRegistrationsNeverExceedTheCoordinatorCap(t *testing.T) {
	store := &fakeStore{sessions: capSessions()}
	svc := capService(store, nil)
	var wg sync.WaitGroup
	results := make([]error, 10)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			in := validInput()
			in.ParentSessionID = "c1"
			in.RemoteSessionID = domain.SessionID(fmt.Sprintf("widgets-%d", i))
			_, results[i] = svc.Register(context.Background(), in)
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range results {
		if err == nil {
			ok++
		} else if codeOf(err) != "COORDINATOR_WORKER_LIMIT_REACHED" {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 3 || len(store.stored) != 3 {
		t.Fatalf("registered %d links (stored %d), want 3", ok, len(store.stored))
	}
}
