package remotereports_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/observe/remotereports"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/remoteworker"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
)

// A worker that reported before this daemon knew about it is linked by
// discovery, and that report reaches the orchestrator exactly once.
func TestDiscoveredWorkerDeliversItsEarlierReportOnce(t *testing.T) {
	ctx := context.Background()

	remoteStore := openStore(t, "widgets")
	remoteReports := reportsvc.New(reportsvc.Deps{Store: remoteStore})
	worker := createSession(t, remoteStore, "widgets", domain.KindWorker)
	if _, err := remoteReports.Create(ctx, reportsvc.CreateInput{SessionID: worker.ID, State: domain.ReportNeedsInput, Note: "Postgres or SQLite?"}); err != nil {
		t.Fatal(err)
	}
	remote := startRemoteDaemon(t, remoteStore, remoteReports, "host-build", "s3cret")

	localStore := openStore(t, "widgets")
	orchestrator := createSession(t, localStore, "widgets", domain.KindOrchestrator)
	delivery := &recordedDelivery{}
	coordinator := reportsvc.NewCoordinator(reportsvc.CoordinatorDeps{Store: localStore, Delivery: delivery})
	entry := remotehost.Entry{Label: "build-box", URL: remote.URL, Password: "s3cret", HostID: "host-build"}
	lookup := func(id string) (remotehost.Entry, bool, error) {
		if id == entry.HostID {
			return entry, true, nil
		}
		return remotehost.Entry{}, false, nil
	}
	links := remoteworker.New(remoteworker.Deps{Store: localStore, SelfHostID: "host-local", Lookup: lookup})
	observer := remotereports.New(remotereports.Deps{
		Store: localStore, Deliverer: coordinator, SelfHostID: "host-local", Lookup: lookup,
		Linker:  links,
		Entries: func() ([]remotehost.Entry, error) { return []remotehost.Entry{entry}, nil },
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	results, err := observer.Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Linked != 1 {
		t.Fatalf("sync = %+v, want one link", results)
	}
	if err := observer.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 || delivery.targets[0] != orchestrator.ID {
		t.Fatalf("submissions = %v targets = %v", delivery.submissions, delivery.targets)
	}
	if msg := delivery.submissions[0]; !strings.Contains(msg, "Postgres or SQLite?") {
		t.Fatalf("delivered = %q", msg)
	}

	// Another pass, with a discovery pass in between, delivers nothing again.
	again, err := observer.Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again[0].Linked != 0 {
		t.Fatalf("second sync linked %d, want 0", again[0].Linked)
	}
	if err := observer.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 {
		t.Fatalf("redelivered: %v", delivery.submissions)
	}
	stored, err := localStore.ListRemoteWorkerLinks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].Source != domain.LinkSourceDiscovered || stored[0].OwnerSessionID != nil || stored[0].HostLabel != "build-box" {
		t.Fatalf("stored links = %+v", stored)
	}
}
