package remotereports_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/observe/remotereports"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/remoteworker"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

type recordedDelivery struct {
	mu          sync.Mutex
	submissions []string
	targets     []domain.SessionID
}

func (d *recordedDelivery) Submit(_ context.Context, target domain.SessionID, message, _ string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.targets = append(d.targets, target)
	d.submissions = append(d.submissions, message)
	return nil
}

func (d *recordedDelivery) Interrupt(context.Context, domain.SessionID) error { return nil }

func openStore(t *testing.T, project string) *sqlite.Store {
	t.Helper()
	s := sqlitetest.MustOpen(t)
	if err := s.UpsertProject(context.Background(), domain.ProjectRecord{ID: project, Path: "/tmp/" + project, RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	return s
}

func createSession(t *testing.T, s *sqlite.Store, project string, kind domain.SessionKind) domain.SessionRecord {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	rec, err := s.CreateSession(context.Background(), domain.SessionRecord{
		ProjectID: domain.ProjectID(project), Kind: kind, Harness: domain.HarnessClaudeCode,
		Activity: domain.Activity{State: domain.ActivityIdle, LastActivityAt: now}, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// startRemoteDaemon serves the remote host's real report routes over HTTP with
// a bearer check standing in for the LAN listener's auth middleware.
func startRemoteDaemon(t *testing.T, store *sqlite.Store, reports *reportsvc.Service, hostID, password string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(remoteDaemonHandler(store, reports, hostID, password))
	t.Cleanup(srv.Close)
	return srv
}

func remoteDaemonHandler(store *sqlite.Store, reports *reportsvc.Service, hostID, password string) http.Handler {
	r := chi.NewRouter()
	r.Route("/api/v1", func(api chi.Router) {
		(&controllers.IdentityController{HostID: hostID}).Register(api)
		api.Group(func(authed chi.Router) {
			authed.Use(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					if req.Header.Get("Authorization") != "Bearer "+password || req.Header.Get("X-AO-Expected-Host-ID") != hostID {
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					next.ServeHTTP(w, req)
				})
			})
			(&controllers.ReportsController{Svc: reports}).Register(authed)
			authed.Get("/sessions", func(w http.ResponseWriter, req *http.Request) {
				rows, err := store.ListAllSessions(req.Context())
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				sessions := []map[string]any{}
				for _, rec := range rows {
					if rec.IsTerminated {
						continue
					}
					sessions = append(sessions, map[string]any{"id": rec.ID, "projectId": rec.ProjectID, "kind": rec.Kind, "displayName": rec.DisplayName, "isTerminated": false})
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"sessions": sessions})
			})
			authed.Get("/sessions/{id}", func(w http.ResponseWriter, req *http.Request) {
				rec, ok, err := store.GetSession(req.Context(), domain.SessionID(chi.URLParam(req, "id")))
				w.Header().Set("Content-Type", "application/json")
				if err != nil || !ok {
					w.WriteHeader(http.StatusNotFound)
					_ = json.NewEncoder(w).Encode(map[string]string{"code": "SESSION_NOT_FOUND"})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"id": rec.ID, "isTerminated": rec.IsTerminated}})
			})
		})
	})
	return r
}

func TestRemoteWorkerReportReachesTheLocalOrchestratorEndToEnd(t *testing.T) {
	ctx := context.Background()

	// Worker host: its own daemon, project and worker; no orchestrator.
	remoteStore := openStore(t, "widgets")
	var clockMu sync.Mutex
	remoteNow := time.Now().UTC()
	remoteClock := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return remoteNow
	}
	remoteReports := reportsvc.New(reportsvc.Deps{Store: remoteStore, Now: remoteClock})
	worker := createSession(t, remoteStore, "widgets", domain.KindWorker)
	remote := startRemoteDaemon(t, remoteStore, remoteReports, "host-build", "s3cret")

	// This machine: the orchestrator's daemon.
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

	// `ao spawn --host build-box` inside the orchestrator recorded this link.
	if _, err := links.Register(ctx, remoteworker.RegisterInput{
		ParentSessionID: orchestrator.ID, HostID: "host-build", RemoteSessionID: worker.ID, RemoteProjectID: "widgets", DisplayName: "auth refactor",
	}); err != nil {
		t.Fatal(err)
	}

	// The worker reports on its own host; nothing there can deliver it.
	created, err := remoteReports.Create(ctx, reportsvc.CreateInput{SessionID: worker.ID, State: domain.ReportNeedsInput, Note: "Postgres or SQLite?"})
	if err != nil {
		t.Fatal(err)
	}
	// A checkpoint is batched for later and must not be pulled before it is due.
	if _, err := remoteReports.Create(ctx, reportsvc.CreateInput{SessionID: worker.ID, State: domain.ReportCheckpoint, Note: "tests green"}); err != nil {
		t.Fatal(err)
	}

	if err := observer.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 || delivery.targets[0] != orchestrator.ID {
		t.Fatalf("submissions = %v targets = %v", delivery.submissions, delivery.targets)
	}
	msg := delivery.submissions[0]
	if !strings.Contains(msg, "[needs_input] auth refactor (session "+string(worker.ID)+" on host build-box)") ||
		!strings.Contains(msg, "Postgres or SQLite?") || strings.Contains(msg, "tests green") {
		t.Fatalf("delivered = %q", msg)
	}
	if dueForLocalPuller(ctx, t, remoteStore, worker.ID, created.ID) {
		t.Fatalf("remote report still due for the local puller after its acknowledgement")
	}

	// A second pass finds nothing new and delivers nothing.
	if err := observer.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 {
		t.Fatalf("redelivered: %v", delivery.submissions)
	}

	// The worker ends while its checkpoint is still batched. A terminated
	// worker's queue is due at once, so this pass still delivers it.
	worker.IsTerminated = true
	if err := remoteStore.UpdateSession(ctx, worker); err != nil {
		t.Fatal(err)
	}
	if err := observer.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 2 || !strings.Contains(delivery.submissions[1], "tests green") {
		t.Fatalf("the batched checkpoint of a terminated worker did not arrive: %v", delivery.submissions)
	}
	if remaining, _ := localStore.ListRemoteWorkerLinks(ctx); len(remaining) != 1 {
		t.Fatal("link retired in the pass that still delivered its report")
	}

	// Nothing is left: the next pass retires the link.
	if err := observer.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if remaining, _ := localStore.ListRemoteWorkerLinks(ctx); len(remaining) != 0 {
		t.Fatalf("link survived the remote worker: %+v", remaining)
	}
}

// TestRemoteReportIsDeliveredOnceAcrossOutagesAndRestarts walks one report
// through an offline host, a lost acknowledgement, and a restart of each
// daemon, with both daemons on real stores, and requires exactly one delivery.
func TestRemoteReportIsDeliveredOnceAcrossOutagesAndRestarts(t *testing.T) {
	ctx := context.Background()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	// A restart reopens the same database file; the schema is already current.
	reopen := func(dir string) *sqlite.Store {
		t.Helper()
		s, err := sqlite.OpenPreMigrated(dir)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	// Worker host.
	remoteDir := t.TempDir()
	remoteStore, err := sqlitetest.Open(remoteDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := remoteStore.UpsertProject(ctx, domain.ProjectRecord{ID: "widgets", Path: "/tmp/widgets", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	worker := createSession(t, remoteStore, "widgets", domain.KindWorker)
	created, err := reportsvc.New(reportsvc.Deps{Store: remoteStore}).Create(ctx, reportsvc.CreateInput{SessionID: worker.ID, State: domain.ReportNeedsInput, Note: "Postgres or SQLite?"})
	if err != nil {
		t.Fatal(err)
	}
	var (
		urlMu     sync.Mutex
		remoteURL string
		failAcks  bool
	)
	serveRemote := func(store *sqlite.Store) *httptest.Server {
		inner := remoteDaemonHandler(store, reportsvc.New(reportsvc.Deps{Store: store}), "host-build", "s3cret")
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			urlMu.Lock()
			fail := failAcks
			urlMu.Unlock()
			if fail && r.URL.Path == "/api/v1/reports/acknowledge" {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			inner.ServeHTTP(w, r)
		}))
		urlMu.Lock()
		remoteURL = srv.URL
		urlMu.Unlock()
		return srv
	}
	lookup := func(id string) (remotehost.Entry, bool, error) {
		urlMu.Lock()
		defer urlMu.Unlock()
		if id == "host-build" {
			return remotehost.Entry{Label: "build-box", URL: remoteURL, Password: "s3cret", HostID: "host-build"}, true, nil
		}
		return remotehost.Entry{}, false, nil
	}
	remoteState := func(store *sqlite.Store) domain.ReportDeliveryState {
		t.Helper()
		rec, ok, err := store.GetReport(ctx, created.ID)
		if err != nil || !ok {
			t.Fatalf("remote report: ok=%v err=%v", ok, err)
		}
		return rec.DeliveryState
	}

	// This machine.
	localDir := t.TempDir()
	localStore, err := sqlitetest.Open(localDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := localStore.UpsertProject(ctx, domain.ProjectRecord{ID: "ultra-factory", Path: "/tmp/ultra-factory", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	orchestrator := createSession(t, localStore, "ultra-factory", domain.KindOrchestrator)
	delivery := &recordedDelivery{}
	startLocal := func(store *sqlite.Store) *remotereports.Observer {
		coordinator := reportsvc.NewCoordinator(reportsvc.CoordinatorDeps{Store: store, Delivery: delivery})
		return remotereports.New(remotereports.Deps{Store: store, Deliverer: coordinator, SelfHostID: "host-local", Lookup: lookup, Logger: quiet})
	}
	observer := startLocal(localStore)

	// The host is offline when the link is made and on the first passes.
	offline := httptest.NewServer(http.NotFoundHandler())
	offline.Close()
	urlMu.Lock()
	remoteURL = offline.URL
	urlMu.Unlock()
	if _, err := remoteworker.New(remoteworker.Deps{Store: localStore, SelfHostID: "host-local", Lookup: lookup}).Register(ctx, remoteworker.RegisterInput{
		ParentSessionID: orchestrator.ID, HostID: "host-build", RemoteSessionID: worker.ID, RemoteProjectID: "widgets", DisplayName: "auth refactor",
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := observer.Poll(ctx); err != nil {
			t.Fatalf("an offline host must not fail the pass: %v", err)
		}
	}
	if len(delivery.submissions) != 0 {
		t.Fatalf("delivered while the host was offline: %v", delivery.submissions)
	}
	if links, _ := localStore.ListRemoteWorkerLinks(ctx); len(links) != 1 {
		t.Fatal("an offline host's link must be kept")
	}

	// The host returns, but its acknowledgement is lost: the report is
	// delivered once and stays pending remotely.
	urlMu.Lock()
	failAcks = true
	urlMu.Unlock()
	remote := serveRemote(remoteStore)
	if err := observer.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 || delivery.targets[0] != orchestrator.ID || !strings.Contains(delivery.submissions[0], "Postgres or SQLite?") {
		t.Fatalf("submissions = %v targets = %v", delivery.submissions, delivery.targets)
	}
	if state := remoteState(remoteStore); state != domain.ReportPending {
		t.Fatalf("remote report state = %s, want pending after a lost acknowledgement", state)
	}

	// The local daemon restarts: the delivery record survives, so the next
	// pass acknowledges the report without delivering it again.
	if err := localStore.Close(); err != nil {
		t.Fatal(err)
	}
	localStore = reopen(localDir)
	t.Cleanup(func() { _ = localStore.Close() })
	observer = startLocal(localStore)
	urlMu.Lock()
	failAcks = false
	urlMu.Unlock()
	if err := observer.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 {
		t.Fatalf("redelivered after a local restart: %v", delivery.submissions)
	}
	if dueForLocalPuller(ctx, t, remoteStore, worker.ID, created.ID) {
		t.Fatalf("remote report still due for the local puller after its acknowledgement")
	}

	// The remote daemon restarts on a new address: the acknowledgement is
	// durable there, so nothing is pulled again.
	remote.Close()
	if err := remoteStore.Close(); err != nil {
		t.Fatal(err)
	}
	remoteStore = reopen(remoteDir)
	t.Cleanup(func() { _ = remoteStore.Close() })
	remote = serveRemote(remoteStore)
	t.Cleanup(remote.Close)
	if err := observer.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 {
		t.Fatalf("redelivered after a remote restart: %v", delivery.submissions)
	}
	if dueForLocalPuller(ctx, t, remoteStore, worker.ID, created.ID) {
		t.Fatalf("remote report due again after the remote restart")
	}
}

// dueForLocalPuller reports whether the remote daemon still lists reportID as
// due for the local puller's pull of sessionID.
func dueForLocalPuller(ctx context.Context, t *testing.T, store *sqlite.Store, sessionID domain.SessionID, reportID string) bool {
	t.Helper()
	svc := reportsvc.New(reportsvc.Deps{Store: store, Now: time.Now})
	result, err := svc.PullFor(ctx, sessionID, reportsvc.PullOptions{PullerID: "host-local"})
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range result.Due {
		if rec.ID == reportID {
			return true
		}
	}
	return false
}

// Two daemons pull one host, alternating polls between worker reports;
// per-puller acknowledgements keep either from starving the other.
func TestTwoInstancesEachReceiveEveryReportOnceWhateverTheirPullOrder(t *testing.T) {
	ctx := context.Background()

	remoteStore := openStore(t, "widgets")
	remoteReports := reportsvc.New(reportsvc.Deps{Store: remoteStore})
	worker := createSession(t, remoteStore, "widgets", domain.KindWorker)
	remote := startRemoteDaemon(t, remoteStore, remoteReports, "host-build", "s3cret")
	lookup := func(id string) (remotehost.Entry, bool, error) {
		if id == "host-build" {
			return remotehost.Entry{Label: "build-box", URL: remote.URL, Password: "s3cret", HostID: "host-build"}, true, nil
		}
		return remotehost.Entry{}, false, nil
	}

	type instance struct {
		observer *remotereports.Observer
		delivery *recordedDelivery
	}
	startInstance := func(selfHostID string) instance {
		store := openStore(t, "ultra-factory")
		orchestrator := createSession(t, store, "ultra-factory", domain.KindOrchestrator)
		delivery := &recordedDelivery{}
		observer := remotereports.New(remotereports.Deps{
			Store: store, Deliverer: reportsvc.NewCoordinator(reportsvc.CoordinatorDeps{Store: store, Delivery: delivery}),
			SelfHostID: selfHostID, Lookup: lookup, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		links := remoteworker.New(remoteworker.Deps{Store: store, SelfHostID: selfHostID, Lookup: lookup})
		if _, err := links.Register(ctx, remoteworker.RegisterInput{
			ParentSessionID: orchestrator.ID, HostID: "host-build", RemoteSessionID: worker.ID, RemoteProjectID: "widgets", DisplayName: "auth refactor",
		}); err != nil {
			t.Fatal(err)
		}
		return instance{observer: observer, delivery: delivery}
	}
	production, dev := startInstance("host-production"), startInstance("host-dev")

	poll := func(in instance) {
		t.Helper()
		if err := in.observer.Poll(ctx); err != nil {
			t.Fatal(err)
		}
	}
	report := func(note string) {
		t.Helper()
		if _, err := remoteReports.Create(ctx, reportsvc.CreateInput{SessionID: worker.ID, State: domain.ReportNeedsInput, Note: note}); err != nil {
			t.Fatal(err)
		}
	}

	report("first")
	poll(production)
	report("second")
	poll(dev)
	poll(production)
	report("third")
	poll(dev)
	poll(production)
	poll(dev)

	for name, in := range map[string]instance{"production": production, "dev": dev} {
		delivered := strings.Join(in.delivery.submissions, "\n")
		for _, note := range []string{"first", "second", "third"} {
			if got := strings.Count(delivered, note); got != 1 {
				t.Errorf("%s instance received %q %d times, want once; deliveries: %q", name, note, got, in.delivery.submissions)
			}
		}
	}
}
