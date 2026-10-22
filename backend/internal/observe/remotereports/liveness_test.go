package remotereports

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
)

func TestLivenessDropsTerminatedWorkerWithNothingPending(t *testing.T) {
	host := newFakeHost(t, "host-build", "pw")
	host.terminated["w-1"] = true
	store := newMemStore(link("host-build", "w-1"), link("host-build", "w-2"))
	if err := newTestObserver(store, &fakeDeliverer{}, host).Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.linkCount() != 1 || store.links[0].RemoteSessionID != "w-2" {
		t.Fatalf("links = %+v, want only w-2 kept", store.links)
	}
}

func TestLivenessDropsWorkerTheHostNoLongerKnows(t *testing.T) {
	host := newFakeHost(t, "host-build", "pw")
	host.gone["w-1"] = true
	store := newMemStore(link("host-build", "w-1"))
	if err := newTestObserver(store, &fakeDeliverer{}, host).Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.linkCount() != 0 {
		t.Fatal("link of a vanished worker kept")
	}
}

func TestLivenessKeepsTerminatedWorkerWithReportsStillPending(t *testing.T) {
	host := newFakeHost(t, "host-build", "pw")
	host.terminated["w-1"] = true
	host.pending["w-1"] = reportsJSON(needsInput("rpt_1", time.Now()))
	store := newMemStore(link("host-build", "w-1"))
	if err := newTestObserver(store, &fakeDeliverer{}, host).Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.linkCount() != 1 || store.links[0].LastState != domain.RemoteLinkTerminated {
		t.Fatalf("links = %+v, want the terminated link kept until its report is pulled", store.links)
	}
	if state := store.links[0].State(time.Now()); state != domain.RemoteLinkTerminated {
		t.Fatalf("state = %s, want terminated so it does not count", state)
	}
}

func TestLivenessRecordsWhenALiveHostAnswered(t *testing.T) {
	host := newFakeHost(t, "host-build", "pw")
	store := newMemStore(link("host-build", "w-1"))
	before := time.Now()
	if err := newTestObserver(store, &fakeDeliverer{}, host).Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	seen := store.links[0].LastSeenAt
	if seen == nil || seen.Before(before) || store.links[0].LastState != domain.RemoteLinkLive {
		t.Fatalf("liveness = %v %q, want a live read this pass", seen, store.links[0].LastState)
	}
}

func TestUnreachableHostKeepsItsLinks(t *testing.T) {
	host := newFakeHost(t, "host-build", "pw")
	host.srv.Close()
	store := newMemStore(link("host-build", "w-1"))
	if err := newTestObserver(store, &fakeDeliverer{}, host).Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.linkCount() != 1 || store.links[0].LastSeenAt != nil {
		t.Fatalf("links = %+v, want the link kept and never seen", store.links)
	}
}

func TestRefusedPasswordIsNotSentAgain(t *testing.T) {
	host := newFakeHost(t, "host-build", "right")
	store := newMemStore(link("host-build", "w-1"))
	observer := New(Deps{
		Store: store, Deliverer: &fakeDeliverer{}, SelfHostID: "host-local",
		Lookup: func(id string) (remotehost.Entry, bool, error) {
			return remotehost.Entry{Label: "label", URL: host.srv.URL, Password: "wrong", HostID: id}, true, nil
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	refusals := host.authenticated()
	if refusals != 1 {
		t.Fatalf("authenticated requests on the refused pass = %d, want the one read", refusals)
	}
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := host.authenticated(); got != refusals {
		t.Fatalf("the refused password was sent again: %d requests", got-refusals)
	}
	if store.linkCount() != 1 {
		t.Fatal("link dropped on a refused password")
	}
}

func TestDiscoveryDoesNotRelinkATerminatedRemovedWorker(t *testing.T) {
	host := newFakeHost(t, "host-build", "pw")
	host.sessionList = sessionsJSON(session("w-1", "worker", "widgets", false))
	host.terminated["w-1"] = true
	store := newMemStore()
	store.removals = []domain.RemoteWorkerLinkRemoval{{HostID: "host-build", RemoteSessionID: "w-1", RemovedAt: time.Now()}}
	linker := newLinker()
	linker.store = store
	observer := newDiscoveryObserver(store, linker, host)
	observer.discoverIfDue(context.Background())
	if store.linkCount() != 0 || len(store.removals) != 1 {
		t.Fatalf("links = %d, removals = %d; the removed terminated worker was linked again", store.linkCount(), len(store.removals))
	}
}

func TestDiscoveryRelinksARemovedWorkerThatIsLiveAgain(t *testing.T) {
	host := newFakeHost(t, "host-build", "pw")
	host.sessionList = sessionsJSON(session("w-1", "worker", "widgets", false))
	store := newMemStore()
	store.removals = []domain.RemoteWorkerLinkRemoval{{HostID: "host-build", RemoteSessionID: "w-1", RemovedAt: time.Now()}}
	linker := newLinker()
	linker.store = store
	observer := newDiscoveryObserver(store, linker, host)
	observer.discoverIfDue(context.Background())
	if store.linkCount() != 1 || len(store.removals) != 0 {
		t.Fatalf("links = %d, removals = %d; a live removed worker should be linked again", store.linkCount(), len(store.removals))
	}
}

// newDiscoveryObserver builds an observer that discovers from one fake host.
func newDiscoveryObserver(store *memStore, linker *fakeLinker, host *fakeHost) *Observer {
	entry := remotehost.Entry{Label: "build", URL: host.srv.URL, Password: host.password, HostID: host.identity}
	return New(Deps{
		Store: store, Deliverer: &fakeDeliverer{}, Linker: linker,
		Lookup:  func(string) (remotehost.Entry, bool, error) { return entry, true, nil },
		Entries: func() ([]remotehost.Entry, error) { return []remotehost.Entry{entry}, nil },
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}
