package remotereports_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/observe/remotereports"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

// failingRecordStore fails the first delivery-record writes, like a full disk.
type failingRecordStore struct {
	*sqlite.Store
	mu       sync.Mutex
	failures int
}

func (s *failingRecordStore) RecordDeliveredRemoteReports(ctx context.Context, host string, id domain.SessionID, ids []string, at time.Time) error {
	s.mu.Lock()
	if s.failures > 0 {
		s.failures--
		s.mu.Unlock()
		return errors.New("disk full")
	}
	s.mu.Unlock()
	return s.Store.RecordDeliveredRemoteReports(ctx, host, id, ids, at)
}

// ownerHarness runs the observer over a real local store and the real
// coordinator against a fake peer daemon serving the real report routes.
type ownerHarness struct {
	t        *testing.T
	local    *sqlite.Store
	remote   *sqlite.Store
	reports  *reportsvc.Service
	delivery *recordedDelivery
	observer *remotereports.Observer
	root     domain.SessionRecord
	// onPull runs once, after the peer answered the first pull.
	onPull func()
}

func newOwnerHarness(t *testing.T, store func(*sqlite.Store) remotereports.Store) *ownerHarness {
	t.Helper()
	h := &ownerHarness{t: t, local: openStore(t, "ultra-factory"), remote: openStore(t, "widgets"), delivery: &recordedDelivery{}}
	h.reports = reportsvc.New(reportsvc.Deps{Store: h.remote})
	h.root = createSession(t, h.local, "ultra-factory", domain.KindOrchestrator)
	inner := remoteDaemonHandler(h.remote, h.reports, "host-build", "s3cret")
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r)
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/reports") {
			once.Do(func() {
				if h.onPull != nil {
					h.onPull()
				}
			})
		}
	}))
	t.Cleanup(srv.Close)
	lookup := func(id string) (remotehost.Entry, bool, error) {
		if id == "host-build" {
			return remotehost.Entry{Label: "build-box", URL: srv.URL, Password: "s3cret", HostID: "host-build"}, true, nil
		}
		return remotehost.Entry{}, false, nil
	}
	coordinator := reportsvc.NewCoordinator(reportsvc.CoordinatorDeps{Store: h.local, Delivery: h.delivery})
	h.observer = remotereports.New(remotereports.Deps{
		Store: store(h.local), Deliverer: coordinator, SelfHostID: "host-local", Lookup: lookup,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return h
}

// link registers a remote worker, owned by owner (nil is the root).
func (h *ownerHarness) link(owner *domain.SessionID) (domain.SessionRecord, string) {
	h.t.Helper()
	ctx := context.Background()
	worker := createSession(h.t, h.remote, "widgets", domain.KindWorker)
	if err := h.local.UpsertRemoteWorkerLink(ctx, domain.RemoteWorkerLink{
		HostID: "host-build", RemoteSessionID: worker.ID, ProjectID: "ultra-factory", RemoteProjectID: "widgets",
		OwnerSessionID: owner, DisplayName: "w-" + string(worker.ID), CreatedAt: time.Now().UTC(),
	}); err != nil {
		h.t.Fatal(err)
	}
	note := "NOTE-" + string(worker.ID)
	if _, err := h.reports.Create(ctx, reportsvc.CreateInput{SessionID: worker.ID, State: domain.ReportNeedsInput, Note: note}); err != nil {
		h.t.Fatal(err)
	}
	return worker, note
}

// coordinator seeds a live coordinator owned by the root.
func (h *ownerHarness) coordinator(scope string) domain.SessionRecord {
	h.t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	rec, err := h.local.CreateSession(context.Background(), domain.SessionRecord{
		ProjectID: "ultra-factory", Kind: domain.KindCoordinator, CoordinatorScope: scope, Harness: domain.HarnessClaudeCode,
		Activity: domain.Activity{State: domain.ActivityIdle, LastActivityAt: now}, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return rec
}

// received counts submissions to target containing needle.
func (h *ownerHarness) received(target domain.SessionID, needle string) int {
	h.delivery.mu.Lock()
	defer h.delivery.mu.Unlock()
	n := 0
	for i, m := range h.delivery.submissions {
		if h.delivery.targets[i] == target && strings.Contains(m, needle) {
			n++
		}
	}
	return n
}

func (h *ownerHarness) total() int {
	h.delivery.mu.Lock()
	defer h.delivery.mu.Unlock()
	return len(h.delivery.submissions)
}

func (h *ownerHarness) poll() {
	h.t.Helper()
	if err := h.observer.Poll(context.Background()); err != nil {
		h.t.Fatal(err)
	}
}

func plainStore(s *sqlite.Store) remotereports.Store { return s }

func TestObserverDeliversOwnedReportsExactlyOnceToTheirOwners(t *testing.T) {
	h := newOwnerHarness(t, plainStore)
	coordA := h.coordinator("area")
	coordB := h.coordinator("area")
	_, noteA := h.link(&coordA.ID)
	_, noteB := h.link(&coordB.ID)
	_, noteRoot := h.link(nil)

	h.poll()
	h.poll()

	if h.received(coordA.ID, noteA) != 1 || h.received(coordB.ID, noteB) != 1 || h.received(h.root.ID, noteRoot) != 1 {
		t.Fatalf("routing wrong: submissions=%v targets=%v", h.delivery.submissions, h.delivery.targets)
	}
	if got := h.total(); got != 3 {
		t.Fatalf("%d deliveries, want exactly 3 (one per owner group)", got)
	}
}

func TestObserverRoutesAnOwnerReassignedMidPassToTheNewOwner(t *testing.T) {
	h := newOwnerHarness(t, plainStore)
	stale := h.coordinator("area")
	next := h.coordinator("area")
	worker, note := h.link(&stale.ID)
	h.onPull = func() {
		if _, _, err := h.local.SetRemoteWorkerOwner(context.Background(), "host-build", worker.ID, &next.ID, domain.OwnershipExplicitReassignment, time.Now().UTC()); err != nil {
			t.Error(err)
		}
	}

	h.poll()
	h.poll()

	if h.received(next.ID, note) != 1 || h.total() != 1 {
		t.Fatalf("submissions=%v targets=%v, want one delivery to %s", h.delivery.submissions, h.delivery.targets, next.ID)
	}
}

func TestObserverRoutesAnExitedOwnersReportToTheRootOnce(t *testing.T) {
	h := newOwnerHarness(t, plainStore)
	owner := h.coordinator("area")
	_, note := h.link(&owner.ID)
	owner.IsTerminated = true
	if err := h.local.UpdateSession(context.Background(), owner); err != nil {
		t.Fatal(err)
	}

	h.poll()
	h.poll()

	if h.received(h.root.ID, note) != 1 || h.received(owner.ID, note) != 0 {
		t.Fatalf("submissions=%v targets=%v, want the report at the root", h.delivery.submissions, h.delivery.targets)
	}
}

func TestObserverDoesNotRedeliverWhenRecordingTheDeliveryFails(t *testing.T) {
	var failing *failingRecordStore
	h := newOwnerHarness(t, func(s *sqlite.Store) remotereports.Store {
		failing = &failingRecordStore{Store: s, failures: 1}
		return failing
	})
	owner := h.coordinator("area")
	worker, note := h.link(&owner.ID)

	h.poll()
	h.poll()
	h.poll()

	if h.received(owner.ID, note) != 1 || h.total() != 1 {
		t.Fatalf("submissions=%v, want exactly one delivery despite the failed record", h.delivery.submissions)
	}
	ids, err := h.local.ListDeliveredRemoteReports(context.Background(), "host-build", worker.ID)
	if err != nil || len(ids) != 1 {
		t.Fatalf("delivered record = %v err=%v, want the retried write to land", ids, err)
	}
}
