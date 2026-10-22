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

// Two daemons on one machine pull the same worker. Each must receive every
// report exactly once, whichever pulls first and however the timers interleave.
func TestTwoDaemonsEachReceiveEveryReportOnce(t *testing.T) {
	ctx := context.Background()
	remoteStore := openStore(t, "widgets")
	worker := createSession(t, remoteStore, "widgets", domain.KindWorker)
	remoteReports := reportsvc.New(reportsvc.Deps{Store: remoteStore})
	remote := startRemoteDaemon(t, remoteStore, remoteReports, "host-build", "s3cret")
	lookup := func(id string) (remotehost.Entry, bool, error) {
		if id == "host-build" {
			return remotehost.Entry{Label: "build-box", URL: remote.URL, Password: "s3cret", HostID: "host-build"}, true, nil
		}
		return remotehost.Entry{}, false, nil
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	type daemon struct {
		observer *remotereports.Observer
		delivery *recordedDelivery
	}
	newDaemon := func(selfHostID string) daemon {
		store := openStore(t, "ultra-factory")
		orchestrator := createSession(t, store, "ultra-factory", domain.KindOrchestrator)
		delivery := &recordedDelivery{}
		coordinator := reportsvc.NewCoordinator(reportsvc.CoordinatorDeps{Store: store, Delivery: delivery})
		links := remoteworker.New(remoteworker.Deps{Store: store, SelfHostID: selfHostID, Lookup: lookup})
		if _, err := links.Register(ctx, remoteworker.RegisterInput{
			ParentSessionID: orchestrator.ID, HostID: "host-build", RemoteSessionID: worker.ID, RemoteProjectID: "widgets", DisplayName: "auth refactor",
		}); err != nil {
			t.Fatal(err)
		}
		observer := remotereports.New(remotereports.Deps{
			Store: store, Deliverer: coordinator, SelfHostID: selfHostID, Lookup: lookup, Logger: quiet,
		})
		return daemon{observer: observer, delivery: delivery}
	}
	first := newDaemon("daemon-a")
	second := newDaemon("daemon-b")

	notes := []string{"question one", "question two", "question three"}
	for _, note := range notes {
		if _, err := remoteReports.Create(ctx, reportsvc.CreateInput{SessionID: worker.ID, State: domain.ReportNeedsInput, Note: note}); err != nil {
			t.Fatal(err)
		}
	}

	// Interleaved passes: the second daemon pulls before the first's next pass.
	for _, d := range []daemon{second, first, first, second, second, first} {
		if err := d.observer.Poll(ctx); err != nil {
			t.Fatal(err)
		}
	}

	for name, d := range map[string]daemon{"daemon-a": first, "daemon-b": second} {
		for _, note := range notes {
			count := 0
			for _, submission := range d.delivery.submissions {
				count += strings.Count(submission, note)
			}
			if count != 1 {
				t.Fatalf("%s received %q %d times, want once; submissions = %q", name, note, count, d.delivery.submissions)
			}
		}
	}
}
