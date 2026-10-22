package remoteworker

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
)

func placementService(store *fakeStore) *Service {
	return New(Deps{Store: store, Lookup: savedHosts(
		remotehost.Entry{Label: "Linux", URL: "https://linux", HostID: "host-linux"},
		remotehost.Entry{Label: "Z600", URL: "https://z600", HostID: "host-z600"},
		remotehost.Entry{Label: "IMac", URL: "https://imac", HostID: "host-imac"},
	)})
}

// slottedSessions has coordinator c1 with slots Linux:2, Z600:1.
func slottedSessions() map[domain.SessionID]domain.SessionRecord {
	three := 3
	return capSessions(domain.SessionRecord{
		ID: "c1", ProjectID: capProject, Kind: domain.KindCoordinator, CoordinatorWorkerLimit: &three,
		CoordinatorHostSlots: []domain.HostSlot{{HostID: "host-linux", Slots: 2}, {HostID: "host-z600", Slots: 1}},
	})
}

func hostLinks(owner domain.SessionID, host string, n int) []domain.RemoteWorkerLink {
	var out []domain.RemoteWorkerLink
	for i := 0; i < n; i++ {
		id := owner
		out = append(out, domain.RemoteWorkerLink{
			HostID: host, RemoteSessionID: domain.SessionID(fmt.Sprintf("%s-%d", host, i)),
			RemoteProjectID: "widgets", ProjectID: capProject, OwnerSessionID: &id, CreatedAt: time.Now(),
		})
	}
	return out
}

func registerOn(svc *Service, host string, n int) error {
	in := validInput()
	in.ParentSessionID = "c1"
	in.HostID = host
	in.RemoteSessionID = domain.SessionID(fmt.Sprintf("new-%d", n))
	_, err := svc.Register(context.Background(), in)
	return err
}

func TestRegisterEnforcesHostSlotsPerCoordinator(t *testing.T) {
	store := &fakeStore{sessions: slottedSessions()}
	svc := placementService(store)
	for i := 0; i < 2; i++ {
		if err := registerOn(svc, "host-linux", i); err != nil {
			t.Fatalf("linux %d: %v", i, err)
		}
	}
	err := registerOn(svc, "host-linux", 3)
	if codeOf(err) != "COORDINATOR_HOST_SLOTS_FULL" {
		t.Fatalf("err = %v, want COORDINATOR_HOST_SLOTS_FULL", err)
	}
	if err := registerOn(svc, "host-imac", 4); codeOf(err) != "COORDINATOR_HOST_NOT_ALLOWED" {
		t.Fatalf("err = %v, want COORDINATOR_HOST_NOT_ALLOWED", err)
	}
	if err := registerOn(svc, "host-z600", 2); err != nil {
		t.Fatalf("z600: %v", err)
	}
	if len(store.stored) != 3 {
		t.Fatalf("a refused registration stored a link: %d links", len(store.stored))
	}
}

func TestTerminatingAWorkerFreesItsHostSlot(t *testing.T) {
	store := &fakeStore{sessions: slottedSessions(), stored: hostLinks("c1", "host-z600", 1)}
	svc := placementService(store)
	if err := registerOn(svc, "host-z600", 1); codeOf(err) != "COORDINATOR_HOST_SLOTS_FULL" {
		t.Fatalf("err = %v, want COORDINATOR_HOST_SLOTS_FULL", err)
	}
	// The observer unlinks a finished worker; its slot is free in the same count.
	store.stored = nil
	if err := registerOn(svc, "host-z600", 2); err != nil {
		t.Fatalf("slot should be free after the link is gone: %v", err)
	}
}

func TestAllowedHostsBoundACoordinatorWithoutSlots(t *testing.T) {
	store := &fakeStore{sessions: capSessions()}
	store.project = domain.ProjectRecord{ID: string(capProject), Config: domain.ProjectConfig{
		Coordinators: domain.CoordinatorsConfig{AllowedHosts: []string{"host-linux"}},
	}}
	svc := placementService(store)
	if err := registerOn(svc, "host-linux", 0); err != nil {
		t.Fatalf("an allowed host: %v", err)
	}
	if err := registerOn(svc, "host-z600", 1); codeOf(err) != "COORDINATOR_HOST_NOT_ALLOWED" {
		t.Fatalf("err = %v, want COORDINATOR_HOST_NOT_ALLOWED", err)
	}
	// Root-owned workers are not placement-checked.
	in := validInput()
	in.HostID = "host-z600"
	in.RemoteSessionID = "root-owned"
	if _, err := svc.Register(context.Background(), in); err != nil {
		t.Fatalf("a root-owned worker ignores allowedHosts: %v", err)
	}
	store.project.Config.Coordinators.AllowedHosts = nil
	if err := registerOn(svc, "host-imac", 2); err != nil {
		t.Fatalf("an empty allow list admits any registered host: %v", err)
	}
}

func TestAllowedHostsStayAHardBoundOverSlots(t *testing.T) {
	store := &fakeStore{sessions: slottedSessions()}
	store.project = domain.ProjectRecord{ID: string(capProject), Config: domain.ProjectConfig{
		Coordinators: domain.CoordinatorsConfig{AllowedHosts: []string{"host-z600"}},
	}}
	if err := registerOn(placementService(store), "host-linux", 0); codeOf(err) != "COORDINATOR_HOST_NOT_ALLOWED" {
		t.Fatalf("err = %v, want COORDINATOR_HOST_NOT_ALLOWED for a slotted host removed from the allow list", err)
	}
}

func TestAssignOwnerChecksTheTargetCoordinatorsHostRules(t *testing.T) {
	onImac := domain.RemoteWorkerLink{HostID: "host-imac", RemoteSessionID: "imac-1", RemoteProjectID: "widgets", ProjectID: capProject, CreatedAt: time.Now()}
	onLinux := domain.RemoteWorkerLink{HostID: "host-linux", RemoteSessionID: "linux-1", RemoteProjectID: "widgets", ProjectID: capProject, CreatedAt: time.Now()}
	store := &fakeStore{sessions: slottedSessions(), stored: []domain.RemoteWorkerLink{onImac, onLinux}}
	svc := placementService(store)
	ctx := context.Background()
	if _, err := svc.AssignOwner(ctx, "host-imac", "imac-1", ownerPtr("c1"), "ao-1"); codeOf(err) != "COORDINATOR_HOST_NOT_ALLOWED" {
		t.Fatalf("err = %v, want COORDINATOR_HOST_NOT_ALLOWED", err)
	}
	if _, err := svc.AssignOwner(ctx, "host-linux", "linux-1", ownerPtr("c1"), "ao-1"); err != nil {
		t.Fatalf("a slotted host: %v", err)
	}
	if _, err := svc.AssignOwner(ctx, "host-imac", "imac-1", nil, "ao-1"); err != nil {
		t.Fatalf("moving to the root is never placement-checked: %v", err)
	}
}

func TestCheckCapacityAppliesHostRulesBeforeAnythingIsSpawned(t *testing.T) {
	store := &fakeStore{sessions: slottedSessions(), stored: hostLinks("c1", "host-z600", 1)}
	svc := placementService(store)
	ctx := context.Background()
	if err := svc.CheckCapacity(ctx, "c1", "host-z600"); codeOf(err) != "COORDINATOR_HOST_SLOTS_FULL" {
		t.Fatalf("err = %v, want COORDINATOR_HOST_SLOTS_FULL", err)
	}
	if err := svc.CheckCapacity(ctx, "c1", "host-imac"); codeOf(err) != "COORDINATOR_HOST_NOT_ALLOWED" {
		t.Fatalf("err = %v, want COORDINATOR_HOST_NOT_ALLOWED", err)
	}
	if err := svc.CheckCapacity(ctx, "c1", "host-linux"); err != nil {
		t.Fatalf("a free slot: %v", err)
	}
	if err := svc.CheckCapacity(ctx, "ao-1", "host-imac"); err != nil {
		t.Fatalf("root-owned parent: %v", err)
	}
	if len(store.stored) != 1 {
		t.Fatalf("a check stored a link: %d", len(store.stored))
	}
}

func TestOfflineHostKeepsItsSlotsCountedUntilTheWorkerEnds(t *testing.T) {
	// Offline is invisible to the count: the link stays, so the slot stays used.
	store := &fakeStore{sessions: slottedSessions(), stored: hostLinks("c1", "host-linux", 2)}
	if err := registerOn(placementService(store), "host-linux", 9); codeOf(err) != "COORDINATOR_HOST_SLOTS_FULL" {
		t.Fatalf("err = %v, want COORDINATOR_HOST_SLOTS_FULL", err)
	}
}
