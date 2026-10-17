package remotereports_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/observe/remotereports"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost/sockstest"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/remoteworker"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
)

// A tailnet host is reached through the SOCKS proxy: every pass opens exactly
// one connection (the identity probe's), reuses it for pulls and
// acknowledgements, and delivers each report once.
func TestTailnetHostPassUsesOneSOCKSConnectionAndDeliversOnce(t *testing.T) {
	ctx := context.Background()

	remoteStore := openStore(t, "widgets")
	remoteReports := reportsvc.New(reportsvc.Deps{Store: remoteStore})
	worker := createSession(t, remoteStore, "widgets", domain.KindWorker)
	remote := startRemoteDaemon(t, remoteStore, remoteReports, "host-build", "s3cret")
	_, port, err := net.SplitHostPort(mustHost(t, remote.URL))
	if err != nil {
		t.Fatal(err)
	}

	// build.ts.net is a tailnet name; the fake proxy tunnels it to the loopback
	// daemon, and records what the observer asked for.
	proxy := sockstest.Start(t, func(string) string { return "127.0.0.1" })
	t.Setenv(remotehost.TailnetSOCKSEnv, proxy.Addr)
	tailnetURL := "http://build.ts.net:" + port

	localStore := openStore(t, "ultra-factory")
	orchestrator := createSession(t, localStore, "ultra-factory", domain.KindOrchestrator)
	delivery := &recordedDelivery{}
	coordinator := reportsvc.NewCoordinator(reportsvc.CoordinatorDeps{Store: localStore, Delivery: delivery})
	lookup := func(id string) (remotehost.Entry, bool, error) {
		if id == "host-build" {
			return remotehost.Entry{Label: "build-box", URL: tailnetURL, Password: "s3cret", HostID: "host-build"}, true, nil
		}
		return remotehost.Entry{}, false, nil
	}
	observer := remotereports.New(remotereports.Deps{
		Store: localStore, Deliverer: coordinator, SelfHostID: "host-local", Lookup: lookup,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	links := remoteworker.New(remoteworker.Deps{Store: localStore, SelfHostID: "host-local", Lookup: lookup})
	if _, err := links.Register(ctx, remoteworker.RegisterInput{
		ParentSessionID: orchestrator.ID, HostID: "host-build", RemoteSessionID: worker.ID, RemoteProjectID: "widgets", DisplayName: "auth refactor",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := remoteReports.Create(ctx, reportsvc.CreateInput{SessionID: worker.ID, State: domain.ReportNeedsInput, Note: "Postgres or SQLite?"}); err != nil {
		t.Fatal(err)
	}

	if err := observer.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 {
		t.Fatalf("first pass submissions = %v, want exactly one", delivery.submissions)
	}
	if got := proxy.Connections(); got != 1 {
		t.Fatalf("first pass opened %d SOCKS connections, want 1 (probe, pull and acknowledge share it)", got)
	}
	for _, target := range proxy.Targets() {
		if target != "build.ts.net:"+port {
			t.Fatalf("SOCKS target = %q, want the unresolved tailnet name", target)
		}
	}

	// The next pass probes again on a new connection, finds nothing new, and
	// delivers nothing a second time.
	if err := observer.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 {
		t.Fatalf("redelivered: %v", delivery.submissions)
	}
	if got := proxy.Connections(); got != 2 {
		t.Fatalf("after two passes SOCKS connections = %d, want 2 (one per pass)", got)
	}
}

// mustHost returns the host:port of a test server URL.
func mustHost(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(u.Host, ":") {
		t.Fatalf("no port in %q", raw)
	}
	return u.Host
}

// The host closes the connection that answered the identity probe. The pull
// must not open a new one, so the saved bearer never reaches the host and the
// report stays pending for a later pass (ADR 0008, Decision item 8).
func TestPassNeverSendsTheBearerOnAConnectionTheProbeDidNotMake(t *testing.T) {
	ctx := context.Background()

	remoteStore := openStore(t, "widgets")
	remoteReports := reportsvc.New(reportsvc.Deps{Store: remoteStore})
	worker := createSession(t, remoteStore, "widgets", domain.KindWorker)
	routes := remoteDaemonHandler(remoteStore, remoteReports, "host-build", "s3cret")
	var bearers atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/identity" {
			w.Header().Set("Connection", "close")
		}
		if r.Header.Get("Authorization") != "" {
			bearers.Add(1)
		}
		routes.ServeHTTP(w, r)
	}))
	t.Cleanup(remote.Close)

	localStore := openStore(t, "ultra-factory")
	orchestrator := createSession(t, localStore, "ultra-factory", domain.KindOrchestrator)
	delivery := &recordedDelivery{}
	coordinator := reportsvc.NewCoordinator(reportsvc.CoordinatorDeps{Store: localStore, Delivery: delivery})
	lookup := func(id string) (remotehost.Entry, bool, error) {
		if id == "host-build" {
			return remotehost.Entry{Label: "build-box", URL: remote.URL, Password: "s3cret", HostID: "host-build"}, true, nil
		}
		return remotehost.Entry{}, false, nil
	}
	observer := remotereports.New(remotereports.Deps{
		Store: localStore, Deliverer: coordinator, SelfHostID: "host-local", Lookup: lookup,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	links := remoteworker.New(remoteworker.Deps{Store: localStore, SelfHostID: "host-local", Lookup: lookup})
	if _, err := links.Register(ctx, remoteworker.RegisterInput{
		ParentSessionID: orchestrator.ID, HostID: "host-build", RemoteSessionID: worker.ID, RemoteProjectID: "widgets", DisplayName: "auth refactor",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := remoteReports.Create(ctx, reportsvc.CreateInput{SessionID: worker.ID, State: domain.ReportNeedsInput, Note: "Postgres or SQLite?"}); err != nil {
		t.Fatal(err)
	}

	if err := observer.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if got := bearers.Load(); got != 0 {
		t.Fatalf("bearer requests on an unprobed connection = %d, want 0", got)
	}
	if len(delivery.submissions) != 0 {
		t.Fatalf("delivered without a pull: %v", delivery.submissions)
	}
}
