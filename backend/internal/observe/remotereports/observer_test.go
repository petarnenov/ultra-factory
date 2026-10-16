package remotereports

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
)

type memStore struct {
	mu        sync.Mutex
	links     []domain.RemoteWorkerLink
	delivered map[string]map[string]bool
	recorded  []string
	recordErr error
	listCalls int
}

func newMemStore(links ...domain.RemoteWorkerLink) *memStore {
	return &memStore{links: links, delivered: map[string]map[string]bool{}}
}

func linkKey(host string, session domain.SessionID) string { return host + "/" + string(session) }

func (m *memStore) ListRemoteWorkerLinks(context.Context) ([]domain.RemoteWorkerLink, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listCalls++
	return append([]domain.RemoteWorkerLink(nil), m.links...), nil
}

func (m *memStore) DeleteRemoteWorkerLink(_ context.Context, host string, session domain.SessionID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.links[:0]
	for _, link := range m.links {
		if link.HostID != host || link.RemoteSessionID != session {
			kept = append(kept, link)
		}
	}
	m.links = kept
	delete(m.delivered, linkKey(host, session))
	return nil
}

func (m *memStore) ListDeliveredRemoteReports(_ context.Context, host string, session domain.SessionID) (map[string]bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]bool{}
	for id := range m.delivered[linkKey(host, session)] {
		out[id] = true
	}
	return out, nil
}

func (m *memStore) RecordDeliveredRemoteReports(_ context.Context, host string, session domain.SessionID, ids []string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.recordErr != nil {
		return m.recordErr
	}
	key := linkKey(host, session)
	if m.delivered[key] == nil {
		m.delivered[key] = map[string]bool{}
	}
	for _, id := range ids {
		m.delivered[key][id] = true
		m.recorded = append(m.recorded, id)
	}
	return nil
}

func (m *memStore) linkCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.links)
}

type delivery struct {
	project domain.ProjectID
	reports []domain.ReportRecord
}

type fakeDeliverer struct {
	mu    sync.Mutex
	calls []delivery
	err   error
}

func (f *fakeDeliverer) DeliverExternal(_ context.Context, project domain.ProjectID, reports []domain.ReportRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, delivery{project, reports})
	return nil
}

type seenRequest struct {
	method, path, auth, expectedHost, body, conn string
}

// fakeHost is a remote AO daemon answering the identity probe and the pull routes.
type fakeHost struct {
	t          *testing.T
	srv        *httptest.Server
	identity   string
	password   string
	mu         sync.Mutex
	requests   []seenRequest
	pending    map[string]string // session -> JSON body or "!<status> <code>"
	terminated map[string]bool
	gone       map[string]bool
	refuseAck  bool
	failAck    bool
	// ackDelay makes every acknowledgement succeed only after this long.
	ackDelay time.Duration
	hang     bool
	hangFor  map[string]bool
	conns    int
	// pendingStatus answers the pending route with this status when set.
	pendingStatus int
	// reportOnSessionGet makes the worker report right as its session is read.
	reportOnSessionGet map[string]string
}

func newFakeHost(t *testing.T, identity, password string) *fakeHost {
	h := &fakeHost{t: t, identity: identity, password: password, pending: map[string]string{}, terminated: map[string]bool{}, gone: map[string]bool{}}
	h.srv = httptest.NewUnstartedServer(http.HandlerFunc(h.serve))
	h.srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			h.mu.Lock()
			h.conns++
			h.mu.Unlock()
		}
	}
	h.srv.Start()
	t.Cleanup(h.srv.Close)
	return h
}

func (h *fakeHost) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	h.mu.Lock()
	h.requests = append(h.requests, seenRequest{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-AO-Expected-Host-ID"), string(body), r.RemoteAddr})
	hang := h.hang
	h.mu.Unlock()
	if r.URL.Path == "/api/v1/identity" {
		writeJSON(w, http.StatusOK, map[string]any{"hostId": h.identity, "apiVersion": remotehost.APIVersion})
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+h.password {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED"})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/reports/pending":
		if hang || h.hangFor[r.URL.Query().Get("sessionId")] {
			h.mu.Unlock()
			<-r.Context().Done()
			h.mu.Lock()
			return
		}
		if h.pendingStatus != 0 {
			writeJSON(w, h.pendingStatus, map[string]string{"code": "TOO_MANY"})
			return
		}
		session := r.URL.Query().Get("sessionId")
		answer, ok := h.pending[session]
		if !ok {
			answer = `{"reports":[]}`
		}
		if strings.HasPrefix(answer, "!") {
			var status int
			var code string
			_, _ = fmt.Sscanf(answer, "!%d %s", &status, &code)
			writeJSON(w, status, map[string]string{"code": code})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(answer))
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/reports/acknowledge":
		if h.refuseAck {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED"})
			return
		}
		if h.failAck {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "DOWN"})
			return
		}
		if delay := h.ackDelay; delay > 0 {
			h.mu.Unlock()
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
			}
			h.mu.Lock()
			if r.Context().Err() != nil {
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"acknowledged": []string{}})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/sessions/"):
		session := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
		if body, ok := h.reportOnSessionGet[session]; ok {
			h.pending[session] = body
		}
		if h.gone[session] {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "SESSION_NOT_FOUND"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"session": map[string]any{"id": session, "isTerminated": h.terminated[session]}})
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "ROUTE_NOT_FOUND"})
	}
}

func (h *fakeHost) setPending(session, body string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pending[session] = body
}

func (h *fakeHost) authenticated() int {
	n := 0
	for _, r := range h.seen() {
		if r.auth != "" {
			n++
		}
	}
	return n
}

func (h *fakeHost) seen() []seenRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]seenRequest(nil), h.requests...)
}

func (h *fakeHost) acks() [][]string {
	var out [][]string
	for _, r := range h.seen() {
		if r.path == "/api/v1/reports/acknowledge" {
			var body struct {
				ReportIDs []string `json:"reportIds"`
			}
			_ = json.Unmarshal([]byte(r.body), &body)
			out = append(out, body.ReportIDs)
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func reportsJSON(reports ...map[string]any) string {
	raw, _ := json.Marshal(map[string]any{"reports": reports})
	return string(raw)
}

func needsInput(id string, at time.Time) map[string]any {
	return map[string]any{"id": id, "sessionId": "widgets-3", "projectId": "widgets", "state": "needs_input", "note": "which DB?", "createdAt": at, "repeatCount": 1}
}

func link(host, session string) domain.RemoteWorkerLink {
	return domain.RemoteWorkerLink{HostID: host, HostLabel: "stored-label", RemoteSessionID: domain.SessionID(session), RemoteProjectID: "widgets", ProjectID: "ultra-factory", DisplayName: "auth refactor", CreatedAt: time.Now()}
}

func newTestObserver(store Store, deliverer Deliverer, hosts ...*fakeHost) *Observer {
	entries := map[string]remotehost.Entry{}
	for _, h := range hosts {
		entries[h.identity] = remotehost.Entry{Label: "label-" + h.identity, URL: h.srv.URL, Password: h.password, HostID: h.identity}
	}
	return New(Deps{
		Store: store, Deliverer: deliverer, SelfHostID: "host-local",
		Lookup: func(id string) (remotehost.Entry, bool, error) {
			entry, ok := entries[id]
			return entry, ok, nil
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func TestPollDeliversRecordsAndAcknowledges(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	host := newFakeHost(t, "host-build", "s3cret")
	host.pending["widgets-3"] = reportsJSON(needsInput("rpt_2", now.Add(time.Second)), needsInput("rpt_1", now))
	host.pending["widgets-4"] = reportsJSON(needsInput("rpt_3", now))
	store := newMemStore(link("host-build", "widgets-3"), link("host-build", "widgets-4"))
	deliverer := &fakeDeliverer{}

	if err := newTestObserver(store, deliverer, host).Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(deliverer.calls) != 1 {
		t.Fatalf("deliveries = %d, want one turn for the project", len(deliverer.calls))
	}
	call := deliverer.calls[0]
	if call.project != "ultra-factory" || len(call.reports) != 3 || call.reports[0].ID != "rpt_1" {
		t.Fatalf("delivery = %+v", call)
	}
	for _, report := range call.reports {
		if report.HostLabel != "label-host-build" || report.SessionDisplayName != "auth refactor" || report.ProjectID != "ultra-factory" || report.State != domain.ReportNeedsInput {
			t.Fatalf("report attribution = %+v", report)
		}
	}
	if len(store.recorded) != 3 {
		t.Fatalf("recorded = %v", store.recorded)
	}
	if acks := host.acks(); len(acks) != 2 {
		t.Fatalf("acks = %v", acks)
	}
	for _, r := range host.seen() {
		if r.path == "/api/v1/identity" {
			if r.auth != "" {
				t.Fatal("identity probe carried the credential")
			}
			continue
		}
		if r.auth != "Bearer s3cret" || r.expectedHost != "host-build" {
			t.Fatalf("%s %s sent auth=%q expected=%q", r.method, r.path, r.auth, r.expectedHost)
		}
	}
}

func TestPollNeverDeliversAReportTwiceButAcknowledgesItAgain(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	host := newFakeHost(t, "host-build", "pw")
	host.pending["widgets-3"] = reportsJSON(needsInput("rpt_1", now))
	store := newMemStore(link("host-build", "widgets-3"))
	deliverer := &fakeDeliverer{}
	observer := newTestObserver(store, deliverer, host)

	// The remote acknowledgement "failed": the report is still listed on the next pass.
	for i := 0; i < 2; i++ {
		if err := observer.Poll(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(deliverer.calls) != 1 {
		t.Fatalf("deliveries = %d, want 1", len(deliverer.calls))
	}
	if acks := host.acks(); len(acks) != 2 || acks[1][0] != "rpt_1" {
		t.Fatalf("acks = %v, want the second pass to acknowledge rpt_1 again", acks)
	}
}

func TestPollLeavesReportsPendingWhenDeliveryFails(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	host := newFakeHost(t, "host-build", "pw")
	host.pending["widgets-3"] = reportsJSON(needsInput("rpt_1", now))
	store := newMemStore(link("host-build", "widgets-3"))
	deliverer := &fakeDeliverer{err: fmt.Errorf("%w: ultra-factory", reportsvc.ErrNoActiveOrchestrator)}

	if err := newTestObserver(store, deliverer, host).Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.recorded) != 0 || len(host.acks()) != 0 {
		t.Fatalf("recorded=%v acks=%v: an undelivered report must stay pending remotely", store.recorded, host.acks())
	}
	if store.linkCount() != 1 {
		t.Fatal("link dropped because delivery failed")
	}
}

func TestPollSkipsAHostWhoseIdentityChangedWithoutSendingItsCredential(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	impostor := newFakeHost(t, "host-impostor", "pw-moved")
	impostor.pending["widgets-3"] = reportsJSON(needsInput("rpt_x", now))
	healthy := newFakeHost(t, "host-z600", "pw-z")
	healthy.pending["z-1"] = reportsJSON(needsInput("rpt_z", now))
	store := newMemStore(link("host-moved", "widgets-3"), link("host-z600", "z-1"))
	deliverer := &fakeDeliverer{}
	observer := newTestObserver(store, deliverer, impostor, healthy)
	// The saved entry for host-moved now points at a daemon that answers as host-impostor.
	inner := observer.lookup
	observer.lookup = func(id string) (remotehost.Entry, bool, error) {
		if id == "host-moved" {
			return remotehost.Entry{Label: "moved", URL: impostor.srv.URL, Password: "pw-moved", HostID: "host-moved"}, true, nil
		}
		return inner(id)
	}

	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, r := range impostor.seen() {
		if r.auth != "" {
			t.Fatalf("credential sent to a host with the wrong identity: %s %s", r.method, r.path)
		}
	}
	if len(deliverer.calls) != 1 || len(deliverer.calls[0].reports) != 1 || deliverer.calls[0].reports[0].ID != "rpt_z" {
		t.Fatalf("the healthy host was not delivered: %+v", deliverer.calls)
	}
	if store.linkCount() != 2 {
		t.Fatal("an unreachable host's link must be kept")
	}
}

func TestPollNeverContactsALinkToThisMachine(t *testing.T) {
	host := newFakeHost(t, "host-local", "pw")
	store := newMemStore(link("host-local", "widgets-3"))
	if err := newTestObserver(store, &fakeDeliverer{}, host).Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(host.seen()) != 0 {
		t.Fatalf("contacted itself: %+v", host.seen())
	}
}

func TestPollDropsLinksOnlyWhenTheRemoteWorkerIsGone(t *testing.T) {
	host := newFakeHost(t, "host-build", "pw")
	host.pending["deleted"] = "!404 SESSION_NOT_FOUND"
	host.pending["old-remote"] = "!404 ROUTE_NOT_FOUND"
	host.terminated["finished"] = true
	host.gone["vanished"] = true
	store := newMemStore(link("host-build", "deleted"), link("host-build", "old-remote"), link("host-build", "finished"),
		link("host-build", "vanished"), link("host-build", "running"))

	if err := newTestObserver(store, &fakeDeliverer{}, host).Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	links, _ := store.ListRemoteWorkerLinks(context.Background())
	var kept []string
	for _, l := range links {
		kept = append(kept, string(l.RemoteSessionID))
	}
	if strings.Join(kept, ",") != "old-remote,running" {
		t.Fatalf("kept links = %v, want old-remote (older AO without the route) and running", kept)
	}
}

func TestPollDropsAMalformedReportWithoutBlockingTheRest(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	host := newFakeHost(t, "host-build", "pw")
	huge := needsInput("rpt_huge", now)
	huge["outputs"] = []map[string]string{{"kind": "artifact", "reference": strings.Repeat("x", maxReferenceRunes+1)}}
	host.pending["widgets-3"] = reportsJSON(
		map[string]any{"id": "rpt_bad", "state": "needs_input", "note": ""},
		huge,
		needsInput("rpt_ok", now),
	)
	store := newMemStore(link("host-build", "widgets-3"))
	deliverer := &fakeDeliverer{}
	if err := newTestObserver(store, deliverer, host).Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(deliverer.calls) != 1 || len(deliverer.calls[0].reports) != 1 || deliverer.calls[0].reports[0].ID != "rpt_ok" {
		t.Fatalf("deliveries = %+v, want only rpt_ok", deliverer.calls)
	}
	acks := host.acks()
	if len(acks) != 1 || strings.Join(acks[0], ",") != "rpt_bad,rpt_huge,rpt_ok" {
		t.Fatalf("acks = %v: undeliverable reports must be settled so they cannot pin the link", acks)
	}
}

func TestPollKeepsATerminatedWorkerUntilItsSettlingReportIsDelivered(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	host := newFakeHost(t, "host-build", "pw")
	host.terminated["widgets-3"] = true
	host.pending["widgets-3"] = `{"reports":[],"morePending":true}`
	store := newMemStore(link("host-build", "widgets-3"))
	deliverer := &fakeDeliverer{}
	observer := newTestObserver(store, deliverer, host)

	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.linkCount() != 1 {
		t.Fatal("link dropped while a done report was still settling on the remote host")
	}
	host.setPending("widgets-3", reportsJSON(needsInput("rpt_done", now)))
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(deliverer.calls) != 1 {
		t.Fatalf("settled report not delivered: %+v", deliverer.calls)
	}
	host.setPending("widgets-3", `{"reports":[],"morePending":false}`)
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.linkCount() != 0 {
		t.Fatal("link kept after the terminated worker had nothing left")
	}
}

func TestPollStopsSendingARefusedPasswordUntilItChanges(t *testing.T) {
	host := newFakeHost(t, "host-build", "rotated")
	store := newMemStore(link("host-build", "w-1"), link("host-build", "w-2"))
	observer := newTestObserver(store, &fakeDeliverer{}, host)
	saved := remotehost.Entry{Label: "build-box", URL: host.srv.URL, Password: "stale", HostID: "host-build"}
	observer.lookup = func(string) (remotehost.Entry, bool, error) { return saved, true, nil }

	for i := 0; i < 3; i++ {
		if err := observer.Poll(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := host.authenticated(); got != 1 {
		t.Fatalf("stale password sent %d times, want once (then wait for it to change)", got)
	}
	saved.Password = "rotated"
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := host.authenticated(); got < 3 {
		t.Fatalf("polling did not resume after the password changed (authenticated requests = %d)", got)
	}
}

func TestPollAcknowledgesEvenWhenRecordingTheDeliveryFails(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	host := newFakeHost(t, "host-build", "pw")
	host.pending["widgets-3"] = reportsJSON(needsInput("rpt_1", now))
	store := newMemStore(link("host-build", "widgets-3"))
	store.recordErr = errors.New("disk full")
	if err := newTestObserver(store, &fakeDeliverer{}, host).Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if acks := host.acks(); len(acks) != 1 || acks[0][0] != "rpt_1" {
		t.Fatalf("acks = %v: acknowledging is what prevents redelivery when the local record failed", acks)
	}
}

func TestAcknowledgeSplitsIntoChunksTheRemoteAccepts(t *testing.T) {
	host := newFakeHost(t, "host-build", "pw")
	client := &hostClient{http: remotehost.DirectClient(nil, time.Second), base: host.srv.URL, hostID: "host-build", password: "pw", label: "b"}
	ids := make([]string, reportsvc.MaxAcknowledgeReports+50)
	for i := range ids {
		ids[i] = fmt.Sprintf("rpt_%d", i)
	}
	if err := client.acknowledge(context.Background(), "w", ids); err != nil {
		t.Fatal(err)
	}
	acks := host.acks()
	if len(acks) != 2 || len(acks[0]) != reportsvc.MaxAcknowledgeReports || len(acks[1]) != 50 {
		t.Fatalf("chunks = %d", len(acks))
	}
}

func TestPollSkipsPlainHTTPHostsUnlessOptedIn(t *testing.T) {
	t.Setenv(remotehost.AllowHTTPEnv, "")
	var logs strings.Builder
	store := newMemStore(link("host-lan", "w-1"))
	deliverer := &fakeDeliverer{}
	observer := New(Deps{
		Store: store, Deliverer: deliverer,
		Lookup: func(string) (remotehost.Entry, bool, error) {
			return remotehost.Entry{Label: "linux", URL: "http://192.168.1.223:1", Password: "pw", HostID: "host-lan"}, true, nil
		},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("a request left for a plain-http host without the opt-in")
			return nil, errors.New("blocked")
		})},
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	})
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), remotehost.AllowHTTPEnv) || store.linkCount() != 1 {
		t.Fatalf("logs = %s", logs.String())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPollNeverLogsThePassword(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	const secret = "correct-horse-battery-staple"
	refusing := newFakeHost(t, "host-refusing", "other-"+secret)
	impostor := newFakeHost(t, "host-impostor", secret)
	healthy := newFakeHost(t, "host-healthy", secret)
	healthy.pending["w-3"] = reportsJSON(needsInput("rpt_1", now))
	var logs strings.Builder
	store := newMemStore(link("host-refusing", "w-1"), link("host-moved", "w-2"), link("host-healthy", "w-3"))
	observer := New(Deps{
		Store: store, Deliverer: &fakeDeliverer{err: errors.New("orchestrator busy")},
		Lookup: func(id string) (remotehost.Entry, bool, error) {
			url := map[string]string{"host-refusing": refusing.srv.URL, "host-moved": impostor.srv.URL, "host-healthy": healthy.srv.URL}[id]
			return remotehost.Entry{Label: id, URL: url, Password: secret, HostID: id}, url != "", nil
		},
		Logger: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if logs.Len() == 0 {
		t.Fatal("expected failures to be logged")
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("password leaked into logs: %s", logs.String())
	}
}

func TestStartPollsAtOnceAndAgainWhenWoken(t *testing.T) {
	store := newMemStore()
	observer := New(Deps{Store: store, Deliverer: &fakeDeliverer{}, Interval: time.Hour, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	ctx, cancel := context.WithCancel(context.Background())
	done := observer.Start(ctx)
	waitFor := func(calls int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			store.mu.Lock()
			got := store.listCalls
			store.mu.Unlock()
			if got >= calls {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("poll %d never happened", calls)
	}
	waitFor(1)
	observer.Wake()
	waitFor(2)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("observer did not stop")
	}
}

func TestPollRequiresDependencies(t *testing.T) {
	if err := New(Deps{}).Poll(context.Background()); err == nil {
		t.Fatal("poll without dependencies succeeded")
	}
	var nilObserver *Observer
	nilObserver.Wake()
	if err := nilObserver.Poll(context.Background()); err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("nil observer err = %v", err)
	}
}

func TestOnePassSendsTheCredentialOnlyOverTheConnectionThatAnsweredTheProbe(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	host := newFakeHost(t, "host-build", "pw")
	host.pending["w-1"] = reportsJSON(needsInput("rpt_1", now))
	host.pending["w-2"] = reportsJSON(needsInput("rpt_2", now))
	store := newMemStore(link("host-build", "w-1"), link("host-build", "w-2"))
	if err := newTestObserver(store, &fakeDeliverer{}, host).Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	requests := host.seen()
	if len(requests) < 5 || requests[0].path != "/api/v1/identity" {
		t.Fatalf("requests = %+v", requests)
	}
	for _, r := range requests[1:] {
		if r.auth != "" && r.conn != requests[0].conn {
			t.Fatalf("%s %s carried the credential on %s, not on the probed connection %s", r.method, r.path, r.conn, requests[0].conn)
		}
	}
	host.mu.Lock()
	conns := host.conns
	host.mu.Unlock()
	if conns != 1 {
		t.Fatalf("connections opened = %d, want 1 for probe, pulls and acknowledgements", conns)
	}
}

func TestASlowHostDoesNotHoldUpTheOthers(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	slow := newFakeHost(t, "host-slow", "pw")
	slow.hang = true
	healthy := newFakeHost(t, "host-healthy", "pw")
	healthy.pending["w-2"] = reportsJSON(needsInput("rpt_2", now))
	store := newMemStore(link("host-slow", "w-1"), link("host-healthy", "w-2"))
	deliverer := &fakeDeliverer{}
	observer := newTestObserver(store, deliverer, slow, healthy)
	observer.hostBudget = 200 * time.Millisecond

	started := time.Now()
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("pass took %s; the slow host was not bounded", elapsed)
	}
	if len(deliverer.calls) != 1 || deliverer.calls[0].reports[0].ID != "rpt_2" || len(healthy.acks()) != 1 {
		t.Fatalf("healthy host not settled: deliveries=%+v acks=%v", deliverer.calls, healthy.acks())
	}
	if store.linkCount() != 2 {
		t.Fatal("the slow host's link must be kept")
	}
}

func TestARefusedAcknowledgementStopsFurtherUseOfThePassword(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	host := newFakeHost(t, "host-build", "pw")
	host.refuseAck = true
	host.pending["w-1"] = reportsJSON(needsInput("rpt_1", now))
	host.pending["w-2"] = reportsJSON(needsInput("rpt_2", now))
	store := newMemStore(link("host-build", "w-1"), link("host-build", "w-2"))
	observer := newTestObserver(store, &fakeDeliverer{}, host)
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if acks := host.acks(); len(acks) != 1 {
		t.Fatalf("acknowledgements sent = %d, want 1: the second must not reuse a refused password", len(acks))
	}
	before := host.authenticated()
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if host.authenticated() != before {
		t.Fatal("the next pass sent the refused password again")
	}
}

func TestPollKeepsAWorkerThatReportedWhileItsSessionWasRead(t *testing.T) {
	host := newFakeHost(t, "host-build", "pw")
	host.terminated["widgets-3"] = true
	host.pending["widgets-3"] = `{"reports":[],"morePending":false}`
	host.reportOnSessionGet = map[string]string{"widgets-3": `{"reports":[],"morePending":true}`}
	store := newMemStore(link("host-build", "widgets-3"))
	if err := newTestObserver(store, &fakeDeliverer{}, host).Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.linkCount() != 1 {
		t.Fatal("link dropped although the ended worker reported between the two reads")
	}
}

func TestSettleStopsAcknowledgingAHostThatFailed(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	host := newFakeHost(t, "host-build", "pw")
	host.failAck = true
	host.pending["w-1"] = reportsJSON(needsInput("rpt_1", now))
	host.pending["w-2"] = reportsJSON(needsInput("rpt_2", now))
	store := newMemStore(link("host-build", "w-1"), link("host-build", "w-2"))
	if err := newTestObserver(store, &fakeDeliverer{}, host).Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if acks := host.acks(); len(acks) != 1 {
		t.Fatalf("acknowledgement attempts = %d, want 1 once the host failed", len(acks))
	}
	if len(store.recorded) != 2 {
		t.Fatalf("recorded = %v: both deliveries must be recorded so the retry does not redeliver", store.recorded)
	}
}

func TestPullTakesAtMostOneAcknowledgementOfReports(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	host := newFakeHost(t, "host-build", "pw")
	host.terminated["w-1"] = true
	var many []map[string]any
	for i := 0; i < maxReportsPerPull+5; i++ {
		many = append(many, needsInput(fmt.Sprintf("rpt_%03d", i), now))
	}
	host.pending["w-1"] = reportsJSON(many...)
	store := newMemStore(link("host-build", "w-1"))
	deliverer := &fakeDeliverer{}
	if err := newTestObserver(store, deliverer, host).Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(deliverer.calls) != 1 || len(deliverer.calls[0].reports) != maxReportsPerPull {
		t.Fatalf("delivered %d reports, want %d", len(deliverer.calls[0].reports), maxReportsPerPull)
	}
	if acks := host.acks(); len(acks) != 1 || len(acks[0]) != maxReportsPerPull {
		t.Fatalf("acks = %d", len(acks))
	}
	if store.linkCount() != 1 {
		t.Fatal("link dropped with reports beyond this pull still waiting")
	}
}

func TestATooManyRequestsAnswerAlsoStopsThePassword(t *testing.T) {
	host := newFakeHost(t, "host-build", "pw")
	host.pendingStatus = http.StatusTooManyRequests
	store := newMemStore(link("host-build", "w-1"), link("host-build", "w-2"))
	observer := newTestObserver(store, &fakeDeliverer{}, host)
	for i := 0; i < 2; i++ {
		if err := observer.Poll(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := host.authenticated(); got != 1 {
		t.Fatalf("authenticated requests after a lockout = %d, want 1", got)
	}
}

func TestAFailedHostIsSkippedForTheRestOfThePassAcrossProjects(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	host := newFakeHost(t, "host-build", "pw")
	host.failAck = true
	host.pending["w-1"] = reportsJSON(needsInput("rpt_1", now))
	host.pending["w-2"] = reportsJSON(needsInput("rpt_2", now))
	first, second := link("host-build", "w-1"), link("host-build", "w-2")
	second.ProjectID = "other-project"
	store := newMemStore(first, second)
	deliverer := &fakeDeliverer{}
	if err := newTestObserver(store, deliverer, host).Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(deliverer.calls) != 2 {
		t.Fatalf("deliveries = %d, want one per project", len(deliverer.calls))
	}
	if acks := host.acks(); len(acks) != 1 {
		t.Fatalf("acknowledgement attempts = %d, want 1 for the whole pass", len(acks))
	}
}

func TestAHostOverItsBudgetIsNotAcknowledgedInTheSamePass(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	host := newFakeHost(t, "host-build", "pw")
	host.pending["w-1"] = reportsJSON(needsInput("rpt_1", now))
	store := newMemStore(link("host-build", "w-1"), link("host-build", "w-2"))
	observer := newTestObserver(store, &fakeDeliverer{}, host)
	observer.hostBudget = 300 * time.Millisecond
	// w-1 answers; w-2 hangs until the budget runs out.
	host.hangFor = map[string]bool{"w-2": true}
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if acks := host.acks(); len(acks) != 0 {
		t.Fatalf("acknowledged a host that ran out of its budget: %v", acks)
	}
}

// A degraded host answers every acknowledgement successfully, but only just
// before the request timeout: ~9 s against the default 10 s. The test keeps
// that ratio at a tenth of the scale (0.9 s against 1 s, a 2 s host budget)
// so it runs in seconds.
func TestSlowButSuccessfulAcknowledgementsStayWithinTheHostBudget(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	const (
		requestTimeout = time.Second
		ackDelay       = 900 * time.Millisecond
		hostBudget     = 2 * time.Second
		slowLinks      = 6
	)
	slow := newFakeHost(t, "host-slow", "pw")
	slow.ackDelay = ackDelay
	healthy := newFakeHost(t, "host-healthy", "pw")
	healthy.pending["h-1"] = reportsJSON(needsInput("rpt_h", now))
	var links []domain.RemoteWorkerLink
	for i := 0; i < slowLinks; i++ {
		session := fmt.Sprintf("s-%d", i)
		slow.pending[session] = reportsJSON(needsInput("rpt_"+session, now))
		l := link("host-slow", session)
		l.ProjectID = domain.ProjectID(fmt.Sprintf("project-%d", i)) // one settle per project
		links = append(links, l)
	}
	last := link("host-healthy", "h-1")
	last.ProjectID = "project-last" // settled after every slow project
	links = append(links, last)
	store := newMemStore(links...)
	deliverer := &fakeDeliverer{}
	observer := newTestObserver(store, deliverer, slow, healthy)
	observer.hostBudget = hostBudget
	observer.reqTimeout = requestTimeout

	started := time.Now()
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(started)

	// Without a budget the slow host's acknowledgements alone would take
	// slowLinks × ackDelay = 5.4 s before the healthy host is settled.
	if elapsed > hostBudget+time.Second {
		t.Fatalf("pass took %s; the slow host's acknowledgements were not bounded by its %s budget", elapsed, hostBudget)
	}
	if attempts := len(slow.acks()); attempts == 0 || attempts > int(hostBudget/ackDelay)+1 {
		t.Fatalf("slow host acknowledgement attempts = %d, want at most %d within the budget", attempts, int(hostBudget/ackDelay)+1)
	}
	if acks := healthy.acks(); len(acks) != 1 || acks[0][0] != "rpt_h" {
		t.Fatalf("healthy host not acknowledged in the same pass: %v", acks)
	}
	if len(deliverer.calls) != slowLinks+1 {
		t.Fatalf("deliveries = %d, want every project delivered (%d)", len(deliverer.calls), slowLinks+1)
	}

	// The next pass acknowledges what waited, without delivering anything twice.
	slow.mu.Lock()
	slow.ackDelay = 0
	slow.mu.Unlock()
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(deliverer.calls) != slowLinks+1 {
		t.Fatalf("redelivered on the next pass: %d deliveries", len(deliverer.calls))
	}
	acked := map[string]bool{}
	for _, ids := range slow.acks() {
		for _, id := range ids {
			acked[id] = true
		}
	}
	if len(acked) != slowLinks {
		t.Fatalf("acknowledged %d of the slow host's %d reports after the next pass", len(acked), slowLinks)
	}
}
