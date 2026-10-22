package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/remoteworker"
)

func TestRemoteWorkerLinkRemovalIsRootOnlyAndForgetsTheLink(t *testing.T) {
	store, worker, root := openReportStore(t)
	ctx := context.Background()
	if err := store.UpsertRemoteWorkerLink(ctx, linkFor("widgets-3")); err != nil {
		t.Fatal(err)
	}
	links := remoteworker.New(remoteworker.Deps{
		Store: store, SelfHostID: "host-local",
		Lookup: func(id string) (remotehost.Entry, bool, error) {
			return remotehost.Entry{Label: "build-box", URL: "http://192.168.1.50:3001", HostID: "host-build"}, id == "host-build", nil
		},
	})
	srv := newRouterServer(t, httpd.APIDeps{RemoteWorkerLinks: links})
	path := "/api/v1/remote-worker-links/host-build/widgets-3"

	body, status, _ := doRequest(t, srv, "DELETE", path+"?requestedBySessionId="+string(worker.ID), "")
	if status != http.StatusForbidden || !reportContainsAll(string(body), "OWNERSHIP_ROOT_ONLY", "requestId") {
		t.Fatalf("non-root removal: status=%d body=%s", status, body)
	}
	if _, ok, _ := store.GetRemoteWorkerLink(ctx, "host-build", "widgets-3"); !ok {
		t.Fatal("a refused removal dropped the link")
	}

	body, status, _ = doRequest(t, srv, "DELETE", path+"?requestedBySessionId="+string(root.ID), "")
	if status != http.StatusOK || !reportContainsAll(string(body), "widgets-3", `"state"`) {
		t.Fatalf("root removal: status=%d body=%s", status, body)
	}
	if _, ok, _ := store.GetRemoteWorkerLink(ctx, "host-build", "widgets-3"); ok {
		t.Fatal("root removal kept the link")
	}
	if _, status, _ = doRequest(t, srv, "DELETE", path+"?requestedBySessionId="+string(root.ID), ""); status != http.StatusNotFound {
		t.Fatalf("second removal status = %d, want 404", status)
	}
}

func TestRemoteWorkerLinkReleaseAfterAKillIsIdempotent(t *testing.T) {
	store, _, _ := openReportStore(t)
	ctx := context.Background()
	if err := store.UpsertRemoteWorkerLink(ctx, linkFor("widgets-4")); err != nil {
		t.Fatal(err)
	}
	links := remoteworker.New(remoteworker.Deps{Store: store, SelfHostID: "host-local"})
	srv := newRouterServer(t, httpd.APIDeps{RemoteWorkerLinks: links})
	path := "/api/v1/remote-worker-links/host-build/widgets-4/release"

	for i := 0; i < 2; i++ {
		if _, status, _ := doRequest(t, srv, "POST", path, ""); status != http.StatusNoContent {
			t.Fatalf("release %d status = %d, want 204", i, status)
		}
	}
	if _, ok, _ := store.GetRemoteWorkerLink(ctx, "host-build", "widgets-4"); ok {
		t.Fatal("release kept the link")
	}
	// Removed, so discovery will not link a terminated worker again.
	removals, err := store.ListRemoteWorkerLinkRemovals(ctx)
	if err != nil || len(removals) != 1 || removals[0].RemoteSessionID != "widgets-4" {
		t.Fatalf("removals = %+v err=%v", removals, err)
	}
}

func linkFor(session string) domain.RemoteWorkerLink {
	return domain.RemoteWorkerLink{
		HostID: "host-build", HostLabel: "build-box", RemoteSessionID: domain.SessionID(session),
		RemoteProjectID: "widgets", ProjectID: "ao", CreatedAt: time.Now().UTC(),
	}
}

// TestRosterCountsLiveLinksAndReportsStaleOnesApart: a live link counts as
// active; a link whose host stopped answering counts as stale, not active.
func TestRosterCountsLiveLinksAndReportsStaleOnesApart(t *testing.T) {
	f := newOwnershipFixture(t)
	srv := f.server(t)
	ctx := context.Background()
	type rosterCapacity struct {
		Capacity struct {
			ActiveWorkers int `json:"activeWorkers"`
			StaleWorkers  int `json:"staleWorkers"`
		} `json:"capacity"`
	}
	read := func() rosterCapacity {
		t.Helper()
		body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/projects/ao/coordinators", "")
		var got rosterCapacity
		if err := json.Unmarshal(body, &got); err != nil || status != http.StatusOK {
			t.Fatalf("status=%d body=%s err=%v", status, body, err)
		}
		return got
	}
	before := read()

	owner := f.coordinator.ID
	live := linkFor("widgets-live")
	live.OwnerSessionID = &owner
	stale := linkFor("widgets-stale")
	stale.OwnerSessionID = &owner
	stale.CreatedAt = time.Now().UTC().Add(-2 * domain.RemoteWorkerLinkStaleAfter)
	for _, link := range []domain.RemoteWorkerLink{live, stale} {
		if err := f.store.UpsertRemoteWorkerLink(ctx, link); err != nil {
			t.Fatal(err)
		}
	}

	after := read()
	if after.Capacity.ActiveWorkers != before.Capacity.ActiveWorkers+1 || after.Capacity.StaleWorkers != 1 {
		t.Fatalf("capacity before=%+v after=%+v, want one more active and one stale", before.Capacity, after.Capacity)
	}
}
