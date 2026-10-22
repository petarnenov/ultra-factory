package controllers_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/remoteworker"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func openReportStore(t *testing.T) (*sqlite.Store, domain.SessionRecord, domain.SessionRecord) {
	t.Helper()
	ctx := context.Background()
	s, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.UpsertProject(ctx, domain.ProjectRecord{ID: "ao", Path: "/tmp/ao", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	newSession := func(kind domain.SessionKind) domain.SessionRecord {
		rec, err := s.CreateSession(ctx, domain.SessionRecord{
			ProjectID: "ao", Kind: kind, Harness: domain.HarnessClaudeCode,
			Activity: domain.Activity{State: domain.ActivityIdle, LastActivityAt: now}, CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		return rec
	}
	return s, newSession(domain.KindWorker), newSession(domain.KindOrchestrator)
}

func newRouterServer(t *testing.T, deps httpd.APIDeps) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, deps, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	return srv
}

func TestReportPullRoutesListDueReportsAndAcknowledgeThemOnce(t *testing.T) {
	store, worker, orchestrator := openReportStore(t)
	reports := reportsvc.New(reportsvc.Deps{Store: store})
	urgent, err := reports.Create(context.Background(), reportsvc.CreateInput{SessionID: worker.ID, State: domain.ReportStuck, Note: "CI is red"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reports.Create(context.Background(), reportsvc.CreateInput{SessionID: worker.ID, State: domain.ReportDone, Note: "settling"}); err != nil {
		t.Fatal(err)
	}
	srv := newRouterServer(t, httpd.APIDeps{Reports: reports})

	body, status, _ := doRequest(t, srv, "GET", "/api/v1/reports/pending?sessionId="+string(worker.ID), "")
	var listed struct {
		Reports []struct {
			ID    string `json:"id"`
			State string `json:"state"`
			Note  string `json:"note"`
		} `json:"reports"`
		MorePending bool `json:"morePending"`
	}
	if err := json.Unmarshal(body, &listed); err != nil || status != http.StatusOK {
		t.Fatalf("status=%d body=%s err=%v", status, body, err)
	}
	if len(listed.Reports) != 1 || listed.Reports[0].ID != urgent.ID || listed.Reports[0].State != "stuck" || listed.Reports[0].Note != "CI is red" {
		t.Fatalf("pending = %s (the done report is still settling and must not be listed)", body)
	}
	if !listed.MorePending {
		t.Fatalf("pending = %s: the settling done report must keep morePending true", body)
	}

	ack := `{"sessionId":"` + string(worker.ID) + `","reportIds":["` + urgent.ID + `"]}`
	body, status, _ = doRequest(t, srv, "POST", "/api/v1/reports/acknowledge", ack)
	if status != http.StatusOK || string(body) != `{"acknowledged":["`+urgent.ID+`"]}`+"\n" {
		t.Fatalf("ack status=%d body=%s", status, body)
	}
	body, status, _ = doRequest(t, srv, "POST", "/api/v1/reports/acknowledge", ack)
	if status != http.StatusOK || string(body) != `{"acknowledged":[]}`+"\n" {
		t.Fatalf("second ack status=%d body=%s", status, body)
	}
	body, _, _ = doRequest(t, srv, "GET", "/api/v1/reports/pending?sessionId="+string(worker.ID), "")
	if string(body) != `{"reports":[],"morePending":true}`+"\n" {
		t.Fatalf("pending after ack = %s", body)
	}

	for _, tc := range []struct {
		method, path, body, code string
		status                   int
	}{
		{"GET", "/api/v1/reports/pending", "", "INVALID_REPORT_SESSION", http.StatusBadRequest},
		{"GET", "/api/v1/reports/pending?sessionId=ghost", "", "SESSION_NOT_FOUND", http.StatusNotFound},
		{"GET", "/api/v1/reports/pending?sessionId=" + string(orchestrator.ID), "", "REPORT_WORKER_REQUIRED", http.StatusBadRequest},
		{"POST", "/api/v1/reports/acknowledge", `{`, "INVALID_JSON", http.StatusBadRequest},
		{"POST", "/api/v1/reports/acknowledge", `{"sessionId":"x","reportIds":["a"],"extra":1}`, "INVALID_JSON", http.StatusBadRequest},
		{"POST", "/api/v1/reports/acknowledge", `{"sessionId":"` + string(worker.ID) + `","reportIds":[]}`, "INVALID_REPORT_IDS", http.StatusBadRequest},
		{"POST", "/api/v1/reports/acknowledge", `{"sessionId":"ghost","reportIds":["a"]}`, "SESSION_NOT_FOUND", http.StatusNotFound},
	} {
		body, status, _ := doRequest(t, srv, tc.method, tc.path, tc.body)
		if status != tc.status || !reportContainsAll(string(body), tc.code, "requestId") {
			t.Fatalf("%s %s: status=%d body=%s, want %d %s", tc.method, tc.path, status, body, tc.status, tc.code)
		}
	}
}

func TestRemoteWorkerLinkRouteStoresTheLinkAgainstTheParentsProject(t *testing.T) {
	store, _, orchestrator := openReportStore(t)
	links := remoteworker.New(remoteworker.Deps{
		Store: store, SelfHostID: "host-local",
		Lookup: func(id string) (remotehost.Entry, bool, error) {
			return remotehost.Entry{Label: "build-box", URL: "http://192.168.1.50:3001", HostID: "host-build"}, id == "host-build", nil
		},
	})
	srv := newRouterServer(t, httpd.APIDeps{RemoteWorkerLinks: links})

	body, status, _ := doRequest(t, srv, "POST", "/api/v1/remote-worker-links",
		`{"parentSessionId":"`+string(orchestrator.ID)+`","hostId":"host-build","remoteSessionId":"widgets-3","remoteProjectId":"widgets","displayName":"auth refactor"}`)
	var got struct {
		HostID, HostLabel, RemoteSessionID, RemoteProjectID, ProjectID, DisplayName, PullBlocked string
	}
	if err := json.Unmarshal(body, &got); err != nil || status != http.StatusCreated {
		t.Fatalf("status=%d body=%s err=%v", status, body, err)
	}
	if got.HostLabel != "build-box" || got.ProjectID != "ao" || got.RemoteSessionID != "widgets-3" || got.RemoteProjectID != "widgets" {
		t.Fatalf("link = %s", body)
	}
	if !strings.Contains(got.PullBlocked, "Allow report pulls over plain HTTP") {
		t.Fatalf("a plain-http LAN host must report why it is not polled: %s", body)
	}
	stored, err := store.ListRemoteWorkerLinks(context.Background())
	if err != nil || len(stored) != 1 || stored[0].ProjectID != "ao" {
		t.Fatalf("stored = %+v err=%v", stored, err)
	}

	for _, tc := range []struct {
		body, code string
		status     int
	}{
		{`{"parentSessionId":"` + string(orchestrator.ID) + `","hostId":"host-other","remoteSessionId":"w","remoteProjectId":"p"}`, "REMOTE_HOST_UNKNOWN", http.StatusBadRequest},
		{`{"parentSessionId":"` + string(orchestrator.ID) + `","hostId":"host-local","remoteSessionId":"w","remoteProjectId":"p"}`, "REMOTE_HOST_IS_LOCAL", http.StatusBadRequest},
		{`{"parentSessionId":"ghost","hostId":"host-build","remoteSessionId":"w","remoteProjectId":"p"}`, "SESSION_NOT_FOUND", http.StatusNotFound},
		{`{"parentSessionId":"x","hostId":"host-build"}`, "INVALID_REMOTE_WORKER_LINK", http.StatusBadRequest},
		{`{"hostLabel":"spoofed"}`, "INVALID_JSON", http.StatusBadRequest},
	} {
		body, status, _ := doRequest(t, srv, "POST", "/api/v1/remote-worker-links", tc.body)
		if status != tc.status || !reportContainsAll(string(body), tc.code, "requestId") {
			t.Fatalf("body %s: status=%d response=%s, want %d %s", tc.body, status, body, tc.status, tc.code)
		}
	}
}

func TestRemoteWorkerCapacityCheckRoute(t *testing.T) {
	store, _, orchestrator := openReportStore(t)
	links := remoteworker.New(remoteworker.Deps{Store: store, SelfHostID: "host-local"})
	srv := newRouterServer(t, httpd.APIDeps{RemoteWorkerLinks: links})

	body, status, _ := doRequest(t, srv, "POST", "/api/v1/remote-worker-links/check",
		`{"parentSessionId":"`+string(orchestrator.ID)+`"}`)
	if status != http.StatusOK || !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("status=%d body=%s", status, body)
	}
	body, status, _ = doRequest(t, srv, "POST", "/api/v1/remote-worker-links/check", `{"parentSessionId":"ghost"}`)
	if status != http.StatusNotFound || !strings.Contains(string(body), "SESSION_NOT_FOUND") {
		t.Fatalf("status=%d body=%s", status, body)
	}
}

func TestRemoteWorkerLinksAreLoopbackOnlyButPullRoutesServeTheLAN(t *testing.T) {
	if !httpd.IsLANControlBlockedPathForTest("/api/v1/remote-worker-links") {
		t.Fatal("a LAN client could make this daemon contact saved hosts with their credentials")
	}
	for _, path := range []string{"/api/v1/reports/pending", "/api/v1/reports/acknowledge"} {
		if httpd.IsLANControlBlockedPathForTest(path) {
			t.Fatalf("%s is blocked on the LAN listener, so a peer daemon can never pull reports", path)
		}
	}
}

func TestCreateReportRefusesAnOversizedBody(t *testing.T) {
	store, worker, _ := openReportStore(t)
	srv := newRouterServer(t, httpd.APIDeps{Reports: reportsvc.New(reportsvc.Deps{Store: store})})
	body := `{"sessionId":"` + string(worker.ID) + `","outputs":[{"kind":"artifact","reference":"` + strings.Repeat("x", 300<<10) + `"}]}`
	response, status, _ := doRequest(t, srv, "POST", "/api/v1/reports", body)
	if status != http.StatusBadRequest || !reportContainsAll(string(response), "INVALID_JSON") {
		t.Fatalf("status=%d body=%.200s", status, response)
	}
}

func TestReportPullRoutesKeepAcknowledgementsPerPuller(t *testing.T) {
	store, worker, _ := openReportStore(t)
	reports := reportsvc.New(reportsvc.Deps{Store: store})
	urgent, err := reports.Create(context.Background(), reportsvc.CreateInput{SessionID: worker.ID, State: domain.ReportStuck, Note: "CI is red"})
	if err != nil {
		t.Fatal(err)
	}
	srv := newRouterServer(t, httpd.APIDeps{Reports: reports})
	pendingFor := func(puller string) string {
		t.Helper()
		body, status, _ := doRequest(t, srv, "GET", "/api/v1/reports/pending?sessionId="+string(worker.ID)+"&pullerId="+puller, "")
		if status != http.StatusOK {
			t.Fatalf("pending for %s: status=%d body=%s", puller, status, body)
		}
		return string(body)
	}

	if body := pendingFor("daemon-a"); !strings.Contains(body, urgent.ID) || !strings.Contains(body, `"pullerAcks":true`) {
		t.Fatalf("pending for daemon-a = %s, want the report and pullerAcks", body)
	}
	ack := `{"sessionId":"` + string(worker.ID) + `","reportIds":["` + urgent.ID + `"],"pullerId":"daemon-a"}`
	if body, status, _ := doRequest(t, srv, "POST", "/api/v1/reports/acknowledge", ack); status != http.StatusOK || !strings.Contains(string(body), urgent.ID) {
		t.Fatalf("per-puller ack status=%d body=%s", status, body)
	}
	if body := pendingFor("daemon-a"); strings.Contains(body, urgent.ID) {
		t.Fatalf("daemon-a receives its acknowledged report again: %s", body)
	}
	if body := pendingFor("daemon-b"); !strings.Contains(body, urgent.ID) {
		t.Fatalf("daemon-b lost the report daemon-a acknowledged: %s", body)
	}
	if body, _, _ := doRequest(t, srv, "GET", "/api/v1/reports/pending?sessionId="+string(worker.ID), ""); strings.Contains(string(body), "pullerAcks") {
		t.Fatalf("a legacy pull must not advertise per-puller acks: %s", body)
	}

	for _, tc := range []struct {
		path, code string
	}{
		{"/api/v1/reports/pending?sessionId=" + string(worker.ID) + "&pullerId=has%20space", "INVALID_PULLER_ID"},
		{"/api/v1/reports/pending?sessionId=" + string(worker.ID) + "&pullerId=d&expiredSeen=-1", "INVALID_EXPIRED_SEEN"},
	} {
		body, status, _ := doRequest(t, srv, "GET", tc.path, "")
		if status != http.StatusBadRequest || !reportContainsAll(string(body), tc.code) {
			t.Fatalf("%s: status=%d body=%s, want 400 %s", tc.path, status, body, tc.code)
		}
	}
}
