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
	s, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
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
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
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
	settled, _, _ := remoteStore.GetReport(ctx, created.ID)
	if settled.DeliveryState != domain.ReportAcknowledged {
		t.Fatalf("remote report state = %s, want acknowledged", settled.DeliveryState)
	}

	// A second pass finds nothing new and delivers nothing.
	if err := observer.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 {
		t.Fatalf("redelivered: %v", delivery.submissions)
	}

	// The worker ends while its checkpoint is still batched: the link must
	// survive until that report is due and delivered.
	worker.IsTerminated = true
	if err := remoteStore.UpdateSession(ctx, worker); err != nil {
		t.Fatal(err)
	}
	if err := observer.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if remaining, _ := localStore.ListRemoteWorkerLinks(ctx); len(remaining) != 1 {
		t.Fatal("link retired while the ended worker still had a pending report")
	}
	clockMu.Lock()
	remoteNow = remoteNow.Add(domain.ReportBatchFallback + time.Second)
	clockMu.Unlock()
	if err := observer.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 2 || !strings.Contains(delivery.submissions[1], "tests green") {
		t.Fatalf("the batched checkpoint never arrived: %v", delivery.submissions)
	}

	// Nothing is left: the link is retired.
	if err := observer.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if remaining, _ := localStore.ListRemoteWorkerLinks(ctx); len(remaining) != 0 {
		t.Fatalf("link survived the remote worker: %+v", remaining)
	}
}
