package remotereports

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/remoteworker"
)

// fakeLinker mirrors the service rules discovery depends on: a project must be
// registered, an existing link is kept as it is, and MaxLinks is enforced.
type fakeLinker struct {
	mu       sync.Mutex
	store    *memStore
	projects map[domain.ProjectID]bool
	limit    int
}

func (f *fakeLinker) LinkDiscovered(_ context.Context, link domain.RemoteWorkerLink) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.projects[link.ProjectID] {
		return false, remoteworker.ErrProjectNotRegistered
	}
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	for _, existing := range f.store.links {
		if existing.HostID == link.HostID && existing.RemoteSessionID == link.RemoteSessionID {
			return false, nil
		}
	}
	if len(f.store.links) >= f.limit {
		return false, remoteworker.ErrLinkLimit
	}
	link.Source = domain.LinkSourceDiscovered
	link.CreatedAt = time.Now()
	f.store.links = append(f.store.links, link)
	return true, nil
}

func sessionsJSON(sessions ...string) string {
	return `{"sessions":[` + joinComma(sessions) + `]}`
}

func joinComma(items []string) string {
	out := ""
	for i, item := range items {
		if i > 0 {
			out += ","
		}
		out += item
	}
	return out
}

func session(id, kind, project string, terminated bool) string {
	return `{"id":"` + id + `","projectId":"` + project + `","kind":"` + kind + `","displayName":"name ` + id + `","isTerminated":` + boolJSON(terminated) + `}`
}

func boolJSON(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// discoveryObserver builds an observer over fake hosts with a fake linker.
func discoveryObserver(store *memStore, linker *fakeLinker, hosts ...*fakeHost) *Observer {
	entries := []remotehost.Entry{}
	byID := map[string]remotehost.Entry{}
	for _, h := range hosts {
		entry := remotehost.Entry{Label: "label-" + h.identity, URL: h.srv.URL, Password: h.password, HostID: h.identity}
		entries = append(entries, entry)
		byID[h.identity] = entry
	}
	linker.store = store
	return New(Deps{
		Store: store, Deliverer: &fakeDeliverer{}, SelfHostID: "host-local",
		Lookup: func(id string) (remotehost.Entry, bool, error) {
			entry, ok := byID[id]
			return entry, ok, nil
		},
		Linker:  linker,
		Entries: func() ([]remotehost.Entry, error) { return entries, nil },
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func newLinker() *fakeLinker {
	return &fakeLinker{projects: map[domain.ProjectID]bool{"widgets": true, "ultra-factory": true}, limit: 200}
}

func findResult(t *testing.T, results []remoteworker.HostSync, hostID string) remoteworker.HostSync {
	t.Helper()
	for _, r := range results {
		if r.HostID == hostID {
			return r
		}
	}
	t.Fatalf("no result for host %s in %+v", hostID, results)
	return remoteworker.HostSync{}
}

func TestSyncLinksOnlyLiveRegisteredWorkers(t *testing.T) {
	host := newFakeHost(t, "host-build", "s3cret")
	host.sessionList = sessionsJSON(
		session("widgets-1", "worker", "widgets", false),
		session("widgets-2", "orchestrator", "widgets", false),
		session("other-1", "worker", "unregistered", false),
		session("widgets-3", "worker", "widgets", true),
	)
	store := newMemStore()
	results, err := discoveryObserver(store, newLinker(), host).Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := findResult(t, results, "host-build")
	if result.Linked != 1 || result.Skipped != 1 || result.Blocked != "" || result.Error != "" {
		t.Fatalf("result = %+v, want linked 1 skipped 1", result)
	}
	if len(store.links) != 1 || store.links[0].RemoteSessionID != "widgets-1" {
		t.Fatalf("links = %+v, want only widgets-1", store.links)
	}
	if store.links[0].Source != domain.LinkSourceDiscovered || store.links[0].OwnerSessionID != nil {
		t.Fatalf("link = %+v, want a root-owned discovered link", store.links[0])
	}
}

func TestSyncKeepsAnExistingOwner(t *testing.T) {
	host := newFakeHost(t, "host-build", "s3cret")
	host.sessionList = sessionsJSON(session("widgets-1", "worker", "widgets", false))
	owner := domain.SessionID("coordinator-1")
	existing := link("host-build", "widgets-1")
	existing.OwnerSessionID = &owner
	store := newMemStore(existing)
	for pass := 0; pass < 3; pass++ {
		results, err := discoveryObserver(store, newLinker(), host).Sync(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if got := findResult(t, results, "host-build"); got.Linked != 0 || got.Skipped != 1 {
			t.Fatalf("pass %d result = %+v", pass, got)
		}
	}
	if len(store.links) != 1 || store.links[0].OwnerSessionID == nil || *store.links[0].OwnerSessionID != owner {
		t.Fatalf("owner changed: %+v", store.links)
	}
}

func TestSyncStopsAtTheLinkLimit(t *testing.T) {
	host := newFakeHost(t, "host-build", "s3cret")
	host.sessionList = sessionsJSON(
		session("widgets-1", "worker", "widgets", false),
		session("widgets-2", "worker", "widgets", false),
		session("widgets-3", "worker", "widgets", false),
	)
	store := newMemStore(link("host-build", "widgets-9"))
	linker := newLinker()
	linker.limit = 2
	results, err := discoveryObserver(store, linker, host).Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := findResult(t, results, "host-build")
	if result.Linked != 1 || result.Error == "" {
		t.Fatalf("result = %+v, want one link and the limit reported", result)
	}
	if len(store.links) != 2 {
		t.Fatalf("links = %d, want the cap of 2", len(store.links))
	}
}

func TestSyncDoesNotRetryARefusedPassword(t *testing.T) {
	host := newFakeHost(t, "host-build", "s3cret")
	host.sessionList = sessionsJSON(session("widgets-1", "worker", "widgets", false))
	store := newMemStore()
	observer := discoveryObserver(store, newLinker(), host)
	observer.entries = func() ([]remotehost.Entry, error) {
		return []remotehost.Entry{{Label: "label", URL: host.srv.URL, Password: "wrong", HostID: "host-build"}}, nil
	}
	observer.lookup = func(string) (remotehost.Entry, bool, error) {
		return remotehost.Entry{Label: "label", URL: host.srv.URL, Password: "wrong", HostID: "host-build"}, true, nil
	}
	for pass := 0; pass < 2; pass++ {
		results, err := observer.Sync(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if got := findResult(t, results, "host-build"); got.Error == "" || got.Linked != 0 {
			t.Fatalf("pass %d result = %+v, want a refusal", pass, got)
		}
	}
	if n := host.authenticated(); n != 1 {
		t.Fatalf("authenticated requests = %d, want one: the refused password is not sent again", n)
	}
	if len(store.links) != 0 {
		t.Fatalf("links = %+v, want none", store.links)
	}
}

func TestSyncReportsABlockedTransportWithoutContactingTheHost(t *testing.T) {
	store := newMemStore()
	observer := New(Deps{
		Store: store, Deliverer: &fakeDeliverer{}, Linker: newLinker(),
		Entries: func() ([]remotehost.Entry, error) {
			return []remotehost.Entry{{Label: "far", URL: "http://203.0.113.7:8080", Password: "s3cret", HostID: "host-far"}}, nil
		},
		Lookup: func(string) (remotehost.Entry, bool, error) {
			return remotehost.Entry{Label: "far", URL: "http://203.0.113.7:8080", Password: "s3cret", HostID: "host-far"}, true, nil
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	results, err := observer.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := findResult(t, results, "host-far"); got.Blocked == "" || got.Linked != 0 {
		t.Fatalf("result = %+v, want blocked", got)
	}
}

func TestSyncOfflineHostDoesNotStopTheOthers(t *testing.T) {
	up := newFakeHost(t, "host-up", "s3cret")
	up.sessionList = sessionsJSON(session("widgets-1", "worker", "widgets", false))
	store := newMemStore()
	linker := newLinker()
	linker.store = store
	observer := New(Deps{
		Store: store, Deliverer: &fakeDeliverer{}, Linker: linker, HostBudget: 2 * time.Second,
		Entries: func() ([]remotehost.Entry, error) {
			return []remotehost.Entry{
				{Label: "down", URL: "http://127.0.0.1:1", Password: "s3cret", HostID: "host-down"},
				{Label: "up", URL: up.srv.URL, Password: "s3cret", HostID: "host-up"},
			}, nil
		},
		Lookup: func(id string) (remotehost.Entry, bool, error) {
			if id == "host-down" {
				return remotehost.Entry{Label: "down", URL: "http://127.0.0.1:1", Password: "s3cret", HostID: "host-down"}, true, nil
			}
			return remotehost.Entry{Label: "up", URL: up.srv.URL, Password: "s3cret", HostID: "host-up"}, true, nil
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	results, err := observer.Sync(context.Background())
	if err != nil {
		t.Fatalf("an offline host failed the pass: %v", err)
	}
	if got := findResult(t, results, "host-down"); got.Error == "" {
		t.Fatalf("offline host result = %+v, want an error", got)
	}
	if got := findResult(t, results, "host-up"); got.Linked != 1 {
		t.Fatalf("online host result = %+v, want one link", got)
	}
}

func TestDiscoverIfDueRunsOnStartThenOnTheInterval(t *testing.T) {
	host := newFakeHost(t, "host-build", "s3cret")
	store := newMemStore()
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	observer := discoveryObserver(store, newLinker(), host)
	observer.now = func() time.Time { return now }
	observer.discoveryInterval = time.Minute

	ctx := context.Background()
	observer.discoverIfDue(ctx)
	observer.discoverIfDue(ctx)
	if n := host.authenticated(); n != 1 {
		t.Fatalf("passes on start = %d, want 1", n)
	}
	now = now.Add(59 * time.Second)
	observer.discoverIfDue(ctx)
	if n := host.authenticated(); n != 1 {
		t.Fatalf("pass before the interval = %d, want 1", n)
	}
	now = now.Add(2 * time.Second)
	observer.discoverIfDue(ctx)
	if n := host.authenticated(); n != 2 {
		t.Fatalf("passes after the interval = %d, want 2", n)
	}
}
