package remoteworker

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
)

func discoveredLink(hostID string, session domain.SessionID, project domain.ProjectID) domain.RemoteWorkerLink {
	return domain.RemoteWorkerLink{HostID: hostID, RemoteSessionID: session, RemoteProjectID: project, ProjectID: project, DisplayName: "auth refactor"}
}

func TestLinkDiscoveredStoresARootOwnedLinkWithTheSavedLabel(t *testing.T) {
	store := &fakeStore{project: domain.ProjectRecord{ID: "widgets"}}
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	woken := 0
	svc := New(Deps{
		Store: store, SelfHostID: "host-local", Now: func() time.Time { return now },
		Lookup:   savedHosts(remotehost.Entry{Label: "build-box", URL: "https://build.example", HostID: "host-build", Password: "pw"}),
		OnLinked: func() { woken++ },
	})
	created, err := svc.LinkDiscovered(context.Background(), discoveredLink("host-build", "widgets-3", "widgets"))
	if err != nil || !created {
		t.Fatalf("created = %v err = %v", created, err)
	}
	if woken != 1 {
		t.Fatalf("observer woken %d times, want 1", woken)
	}
	if len(store.stored) != 1 {
		t.Fatalf("stored = %d", len(store.stored))
	}
	got := store.stored[0]
	if got.HostLabel != "build-box" || got.OwnerSessionID != nil || got.Source != domain.LinkSourceDiscovered || !got.CreatedAt.Equal(now) {
		t.Fatalf("link = %+v", got)
	}
}

func TestLinkDiscoveredNeverOverwritesAnExistingOwner(t *testing.T) {
	coordinator := domain.SessionID("coord-1")
	store := &fakeStore{project: domain.ProjectRecord{ID: "widgets"}, stored: []domain.RemoteWorkerLink{
		{HostID: "host-build", RemoteSessionID: "widgets-3", RemoteProjectID: "widgets", ProjectID: "widgets", OwnerSessionID: &coordinator, Source: domain.LinkSourceSpawn},
	}}
	svc := New(Deps{Store: store, Lookup: savedHosts(remotehost.Entry{Label: "build-box", URL: "https://b", HostID: "host-build"})})
	for pass := 0; pass < 3; pass++ {
		created, err := svc.LinkDiscovered(context.Background(), discoveredLink("host-build", "widgets-3", "widgets"))
		if err != nil || created {
			t.Fatalf("pass %d: created = %v err = %v", pass, created, err)
		}
	}
	if len(store.stored) != 1 || store.stored[0].OwnerSessionID == nil || *store.stored[0].OwnerSessionID != coordinator || store.stored[0].Source != domain.LinkSourceSpawn {
		t.Fatalf("stored = %+v, want the spawn link unchanged", store.stored)
	}
}

func TestLinkDiscoveredRefusesUnregisteredProjectsAndUnsavedHosts(t *testing.T) {
	store := &fakeStore{}
	svc := New(Deps{Store: store, SelfHostID: "host-local", Lookup: savedHosts(remotehost.Entry{Label: "build-box", URL: "https://b", HostID: "host-build"})})
	if _, err := svc.LinkDiscovered(context.Background(), discoveredLink("host-build", "widgets-3", "widgets")); !errors.Is(err, ErrProjectNotRegistered) {
		t.Fatalf("unregistered project: err = %v", err)
	}
	store.project = domain.ProjectRecord{ID: "widgets"}
	if _, err := svc.LinkDiscovered(context.Background(), discoveredLink("host-other", "widgets-3", "widgets")); err == nil {
		t.Fatal("an unsaved host was linked")
	}
	if _, err := svc.LinkDiscovered(context.Background(), discoveredLink("host-local", "widgets-3", "widgets")); err == nil {
		t.Fatal("this machine was linked")
	}
	if _, err := svc.LinkDiscovered(context.Background(), discoveredLink("host-build", "bad id!", "widgets")); !errors.Is(err, domain.ErrInvalidRemoteWorkerLink) {
		t.Fatalf("invalid remote session id: err = %v", err)
	}
	if len(store.stored) != 0 {
		t.Fatalf("stored = %+v, want none", store.stored)
	}
}

func TestLinkDiscoveredStopsAtTheLimit(t *testing.T) {
	store := &fakeStore{project: domain.ProjectRecord{ID: "widgets"}}
	for i := 0; i < MaxLinks; i++ {
		store.stored = append(store.stored, domain.RemoteWorkerLink{HostID: "host-build", RemoteSessionID: domain.SessionID(fmt.Sprintf("old-%d", i))})
	}
	svc := New(Deps{Store: store, Lookup: savedHosts(remotehost.Entry{Label: "build-box", URL: "https://b", HostID: "host-build"})})
	if _, err := svc.LinkDiscovered(context.Background(), discoveredLink("host-build", "widgets-3", "widgets")); !errors.Is(err, ErrLinkLimit) {
		t.Fatalf("err = %v, want ErrLinkLimit", err)
	}
	if len(store.stored) != MaxLinks {
		t.Fatalf("stored = %d, want the cap", len(store.stored))
	}
}
