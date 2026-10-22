package remotereports_test

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"sort"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/observe/remotereports"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/remoteworker"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
)

// Two instances that register the same project and read the same saved host
// link the same live workers into their own databases.
func TestTwoInstancesConvergeOnTheSameProjectLinks(t *testing.T) {
	ctx := context.Background()

	remoteStore := openStore(t, "widgets")
	remoteReports := reportsvc.New(reportsvc.Deps{Store: remoteStore})
	createSession(t, remoteStore, "widgets", domain.KindWorker)
	createSession(t, remoteStore, "widgets", domain.KindWorker)
	remote := startRemoteDaemon(t, remoteStore, remoteReports, "host-build", "s3cret")

	entry := remotehost.Entry{Label: "build-box", URL: remote.URL, Password: "s3cret", HostID: "host-build"}
	lookup := func(id string) (remotehost.Entry, bool, error) {
		if id == entry.HostID {
			return entry, true, nil
		}
		return remotehost.Entry{}, false, nil
	}

	production := discoveredLinkIdentities(ctx, t, lookup, entry)
	dev := discoveredLinkIdentities(ctx, t, lookup, entry)

	if len(production) != 2 {
		t.Fatalf("production links = %v, want both workers", production)
	}
	if !reflect.DeepEqual(production, dev) {
		t.Fatalf("instances diverged:\nproduction = %v\ndev        = %v", production, dev)
	}
}

// discoveredLinkIdentities runs one discovery pass in a fresh instance that
// has the project and returns its link identities, sorted.
func discoveredLinkIdentities(ctx context.Context, t *testing.T, lookup func(string) (remotehost.Entry, bool, error), entry remotehost.Entry) []string {
	t.Helper()
	store := openStore(t, "widgets")
	links := remoteworker.New(remoteworker.Deps{Store: store, SelfHostID: "host-local", Lookup: lookup})
	coordinator := reportsvc.NewCoordinator(reportsvc.CoordinatorDeps{Store: store, Delivery: &recordedDelivery{}})
	observer := remotereports.New(remotereports.Deps{
		Store: store, Deliverer: coordinator, SelfHostID: "host-local", Lookup: lookup,
		Linker:  links,
		Entries: func() ([]remotehost.Entry, error) { return []remotehost.Entry{entry}, nil },
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if _, err := observer.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	stored, err := store.ListRemoteWorkerLinks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identities := make([]string, 0, len(stored))
	for _, link := range stored {
		identities = append(identities, link.HostID+"/"+string(link.RemoteSessionID)+"/"+string(link.ProjectID))
	}
	sort.Strings(identities)
	return identities
}
