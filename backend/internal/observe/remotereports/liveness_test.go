package remotereports

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
)

func TestLivenessDropsTerminatedWorkerWithNothingPending(t *testing.T) {
	host := newFakeHost(t, "host-build", "pw")
	host.terminated["w-1"] = true
	store := newMemStore(endedLongAgo(link("host-build", "w-1")), link("host-build", "w-2"))
	if err := newTestObserver(store, &fakeDeliverer{}, host).Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.linkCount() != 1 || store.links[0].RemoteSessionID != "w-2" {
		t.Fatalf("links = %+v, want only w-2 kept", store.links)
	}
}

func TestLivenessKeepsARecentlyEndedWorkerForLatePROutcomes(t *testing.T) {
	host := newFakeHost(t, "host-build", "pw")
	host.terminated["w-1"] = true
	store := newMemStore(link("host-build", "w-1"))
	observer := newTestObserver(store, &fakeDeliverer{}, host)
	for range 2 {
		if err := observer.Poll(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if store.linkCount() != 1 || store.links[0].TerminatedAt == nil {
		t.Fatalf("links = %+v, want the ended worker kept with its end time", store.links)
	}

	observer.now = func() time.Time { return time.Now().Add(domain.TerminatedPRRefreshWindow + time.Hour) }
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.linkCount() != 0 {
		t.Fatalf("links = %+v, want the link dropped after the PR refresh window", store.links)
	}
}

func TestLivenessInitializesLegacyTerminatedAt(t *testing.T) {
	host := newFakeHost(t, "host-build", "pw")
	host.terminated["w-1"] = true
	legacy := link("host-build", "w-1")
	legacy.LastState = domain.RemoteLinkTerminated
	store := newMemStore(legacy)
	if err := newTestObserver(store, &fakeDeliverer{}, host).Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.linkCount() != 1 || store.links[0].TerminatedAt == nil {
		t.Fatalf("links = %+v, want legacy termination time initialized", store.links)
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

// A worker an operator removed by hand is not linked by the periodic pass
// even though the host lists it live, and no detail read is spent on it.
func TestPeriodicDiscoveryDoesNotRelinkAHandRemovedWorker(t *testing.T) {
	host := newFakeHost(t, "host-build", "pw")
	host.sessionList = sessionsJSON(session("w-1", "worker", "widgets", false))
	store := newMemStore()
	store.removals = []domain.RemoteWorkerLinkRemoval{{HostID: "host-build", RemoteSessionID: "w-1", RemovedAt: time.Now()}}
	linker := newLinker()
	linker.store = store
	observer := newDiscoveryObserver(store, linker, host)
	observer.discoverIfDue(context.Background())
	if store.linkCount() != 0 || len(store.removals) != 1 {
		t.Fatalf("links = %d, removals = %d; the removed worker was linked again", store.linkCount(), len(store.removals))
	}
	for _, request := range host.seen() {
		if request.path == "/api/v1/sessions/w-1" {
			t.Fatalf("the removed worker was read again: %+v", request)
		}
	}
}

// An operator's sync links a removed live worker again and forgets the
// removal, so the periodic pass keeps the link from then on.
func TestOperatorSyncRelinksAHandRemovedWorker(t *testing.T) {
	host := newFakeHost(t, "host-build", "pw")
	host.sessionList = sessionsJSON(session("w-1", "worker", "widgets", false))
	store := newMemStore()
	store.removals = []domain.RemoteWorkerLinkRemoval{{HostID: "host-build", RemoteSessionID: "w-1", RemovedAt: time.Now()}}
	linker := newLinker()
	linker.store = store
	observer := newDiscoveryObserver(store, linker, host)
	if _, err := observer.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.linkCount() != 1 || len(store.removals) != 0 {
		t.Fatalf("links = %d, removals = %d; an operator sync should link the removed worker again", store.linkCount(), len(store.removals))
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

// A terminated link on a plain-http host without the opt-in must not have its
// report contents read: the pending route stays unrequested and the link stays.
func TestTerminatedLinkOnABlockedPlainHTTPHostReadsNoReports(t *testing.T) {
	t.Setenv(remotehost.AllowHTTPEnv, "")
	store := newMemStore(link("host-lan", "w-1"))
	var paths []string
	observer := New(Deps{
		Store: store, Deliverer: &fakeDeliverer{},
		Lookup: func(string) (remotehost.Entry, bool, error) {
			return remotehost.Entry{Label: "linux", URL: "http://192.168.1.223:1", Password: "pw", HostID: "host-lan"}, true, nil
		},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			paths = append(paths, r.URL.Path)
			return nil, errors.New("blocked")
		})},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if path == "/api/v1/reports/pending" {
			t.Fatal("report contents were read from a blocked plain-http host")
		}
	}
	if store.linkCount() != 1 {
		t.Fatal("link dropped on a blocked host")
	}
}

// The guard also holds when the policy is checked on the client itself: a
// terminated worker with an opt-in host and nothing pending is dropped.
func TestEndedWithNothingPendingChecksThePolicyOnTheClient(t *testing.T) {
	store := newMemStore(link("host-lan", "w-1"))
	observer := New(Deps{Store: store, Deliverer: &fakeDeliverer{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	blocked := &hostClient{base: "http://192.168.1.223:1", hostID: "host-lan", password: "pw", label: "linux"}
	if observer.endedWithNothingPending(context.Background(), blocked, store.links[0]) {
		t.Fatal("a blocked client was allowed to drop after a pending read")
	}
	if store.linkCount() != 1 {
		t.Fatal("link dropped on a blocked client")
	}
}

func TestPollPullsLiveLinksFirstAndEndedLinksOnASlowCadence(t *testing.T) {
	host := newFakeHost(t, "host-build", "pw")
	host.terminated["ended"] = true
	ended := link("host-build", "ended")
	ended.LastState = domain.RemoteLinkTerminated
	endedAt := time.Now().Add(-time.Hour)
	ended.TerminatedAt = &endedAt
	store := newMemStore(ended, link("host-build", "live"))
	observer := newTestObserver(store, &fakeDeliverer{}, host)
	for range 3 {
		if err := observer.Poll(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	var order []string
	for _, r := range host.seen() {
		for _, id := range []string{"live", "ended"} {
			if strings.HasSuffix(r.path, "/reports/pending") && strings.Contains("&"+r.query+"&", "&sessionId="+id+"&") {
				order = append(order, id)
			}
		}
	}
	if strings.Join(order, ",") != "live,ended,live,live" {
		t.Fatalf("pending pulls = %v, want the ended link once and after the live one", order)
	}
	if store.linkCount() != 2 {
		t.Fatalf("links = %+v, want the recently ended link kept", store.links)
	}
}
