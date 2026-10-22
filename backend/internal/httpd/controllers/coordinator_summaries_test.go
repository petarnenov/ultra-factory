package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func summaryRouteFixture(t *testing.T) (*sqlite.Store, domain.SessionRecord, domain.SessionRecord, domain.SessionRecord, *httpd.APIDeps) {
	t.Helper()
	store, worker, _ := openReportStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	coordinator, err := store.CreateSession(ctx, domain.SessionRecord{
		ProjectID: "ao", Kind: domain.KindCoordinator, Harness: domain.HarnessClaudeCode, CoordinatorScope: "auth area",
		Activity: domain.Activity{State: domain.ActivityIdle, LastActivityAt: now}, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	owned, err := store.CreateSession(ctx, domain.SessionRecord{
		ProjectID: "ao", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode, OwnerSessionID: &coordinator.ID,
		Activity: domain.Activity{State: domain.ActivityIdle, LastActivityAt: now}, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	deps := &httpd.APIDeps{Summaries: reportsvc.NewSummaryService(reportsvc.SummaryDeps{Store: store})}
	return store, coordinator, owned, worker, deps
}

func TestCoordinatorSummaryRoutesSubmitReplayAndInspect(t *testing.T) {
	_, coordinator, owned, rootOwned, deps := summaryRouteFixture(t)
	srv := newRouterServer(t, *deps)
	post := func(body string) ([]byte, int) {
		resp, status, _ := doRequest(t, srv, "POST", "/api/v1/coordinator-summaries", body)
		return resp, status
	}
	body := `{"coordinatorSessionId":"` + string(coordinator.ID) + `","submissionKey":"k1","state":"working","workerSessionIds":["` + string(owned.ID) + `"],"result":"half done"}`

	resp, status := post(body)
	var created struct {
		ID               string   `json:"id"`
		Scope            string   `json:"scope"`
		WorkerSessionIDs []string `json:"workerSessionIds"`
		DeliveryState    string   `json:"deliveryState"`
		Escalation       bool     `json:"escalation"`
	}
	if err := json.Unmarshal(resp, &created); err != nil || status != http.StatusCreated {
		t.Fatalf("status=%d body=%s err=%v", status, resp, err)
	}
	if created.Scope != "auth area" || created.DeliveryState != "pending" || created.Escalation || len(created.WorkerSessionIDs) != 1 || created.WorkerSessionIDs[0] != string(owned.ID) {
		t.Fatalf("created = %+v", created)
	}

	resp, status = post(body)
	var replay struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(resp, &replay); err != nil || status != http.StatusOK || replay.ID != created.ID {
		t.Fatalf("replay status=%d body=%s", status, resp)
	}

	resp, status, _ = doRequest(t, srv, "GET", "/api/v1/sessions/"+string(coordinator.ID)+"/summaries?limit=5", "")
	var history struct {
		Summaries []struct {
			ID string `json:"id"`
		} `json:"summaries"`
	}
	if err := json.Unmarshal(resp, &history); err != nil || status != http.StatusOK || len(history.Summaries) != 1 || history.Summaries[0].ID != created.ID {
		t.Fatalf("history status=%d body=%s", status, resp)
	}

	for name, tc := range map[string]struct {
		path, body, code string
		status           int
	}{
		"worker submitter":  {"/api/v1/coordinator-summaries", `{"coordinatorSessionId":"` + string(rootOwned.ID) + `","submissionKey":"w","state":"working"}`, "SUMMARY_COORDINATOR_REQUIRED", http.StatusForbidden},
		"foreign worker":    {"/api/v1/coordinator-summaries", `{"coordinatorSessionId":"` + string(coordinator.ID) + `","submissionKey":"f","state":"working","workerSessionIds":["` + string(rootOwned.ID) + `"]}`, "SUMMARY_WORKER_NOT_OWNED", http.StatusBadRequest},
		"unknown field":     {"/api/v1/coordinator-summaries", `{"coordinatorSessionId":"x","submissionKey":"u","state":"working","recipient":"other"}`, "INVALID_JSON", http.StatusBadRequest},
		"history of worker": {"/api/v1/sessions/" + string(rootOwned.ID) + "/summaries", "", "SUMMARY_COORDINATOR_REQUIRED", http.StatusBadRequest},
	} {
		method := "POST"
		if tc.body == "" {
			method = "GET"
		}
		resp, status, _ := doRequest(t, srv, method, tc.path, tc.body)
		if status != tc.status || !strings.Contains(string(resp), tc.code) {
			t.Fatalf("%s: status=%d body=%s, want %d %s", name, status, resp, tc.status, tc.code)
		}
	}
}

func TestCoordinatorSummaryRouteRejectsClientTerminatedEscalation(t *testing.T) {
	_, coordinator, _, _, deps := summaryRouteFixture(t)
	srv := newRouterServer(t, *deps)
	body := `{"coordinatorSessionId":"` + string(coordinator.ID) + `","submissionKey":"k1","state":"blocked","escalationReason":"coordinator_terminated"}`
	resp, status, _ := doRequest(t, srv, "POST", "/api/v1/coordinator-summaries", body)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", status, resp)
	}
}

func TestCoordinatorSummaryRouteAcceptsHostQualifiedRemoteWorker(t *testing.T) {
	store, coordinator, _, _, deps := summaryRouteFixture(t)
	ctx := context.Background()
	if err := store.UpsertRemoteWorkerLink(ctx, domain.RemoteWorkerLink{
		HostID: "host-build", HostLabel: "Air builder", RemoteSessionID: "shared-worker",
		RemoteProjectID: "remote-project", ProjectID: "ao", OwnerSessionID: &coordinator.ID, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	srv := newRouterServer(t, *deps)
	body := `{"coordinatorSessionId":"` + string(coordinator.ID) + `","submissionKey":"remote-worker","state":"working","workerSessionIds":["host-build:shared-worker"]}`
	resp, status, _ := doRequest(t, srv, http.MethodPost, "/api/v1/coordinator-summaries", body)
	var created struct {
		WorkerSessionIDs []string `json:"workerSessionIds"`
		RemoteWorkers    []struct {
			HostID          string `json:"hostId"`
			HostLabel       string `json:"hostLabel"`
			RemoteSessionID string `json:"remoteSessionId"`
		} `json:"remoteWorkers"`
	}
	if err := json.Unmarshal(resp, &created); err != nil || status != http.StatusCreated || len(created.WorkerSessionIDs) != 0 || len(created.RemoteWorkers) != 1 {
		t.Fatalf("status=%d body=%s err=%v", status, resp, err)
	}
	if created.RemoteWorkers[0].HostID != "host-build" || created.RemoteWorkers[0].HostLabel != "Air builder" ||
		created.RemoteWorkers[0].RemoteSessionID != "shared-worker" {
		t.Fatalf("remote workers = %+v", created.RemoteWorkers)
	}
}

func TestCoordinatorSummaryRouteRejectsReservedSubmissionKey(t *testing.T) {
	_, coordinator, _, _, deps := summaryRouteFixture(t)
	srv := newRouterServer(t, *deps)
	body := `{"coordinatorSessionId":"` + string(coordinator.ID) + `","submissionKey":"daemon:coordinator_terminated:1","state":"working"}`
	resp, status, _ := doRequest(t, srv, "POST", "/api/v1/coordinator-summaries", body)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", status, resp)
	}
}

func TestCoordinatorSummaryRouteReadyPRsAcceptedAndRejected(t *testing.T) {
	store, coordinator, owned, _, deps := summaryRouteFixture(t)
	srv := newRouterServer(t, *deps)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	pr := domain.PullRequest{
		URL: "https://github.com/o/r/pull/12", SessionID: owned.ID, Number: 12, HeadSHA: "headsha1", UpdatedAt: now,
		Provider: "github", Host: "github.com", Repo: "o/r",
	}
	if err := store.WriteSCMObservation(ctx, pr, nil, nil, nil, nil, ports.ReviewWritePreserve); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertReview(ctx, domain.Review{ID: "rev-1", SessionID: owned.ID, ProjectID: "ao", Harness: domain.ReviewerClaudeCode, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertReviewRun(ctx, domain.ReviewRun{
		ID: "run-1", ReviewID: "rev-1", SessionID: owned.ID, Harness: domain.ReviewerClaudeCode, PRURL: pr.URL, TargetSHA: "headsha1",
		Status: domain.ReviewRunComplete, Verdict: domain.VerdictApproved, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	post := func(key, ready string) ([]byte, int) {
		body := `{"coordinatorSessionId":"` + string(coordinator.ID) + `","submissionKey":"` + key + `","state":"done","readyPRs":[` + ready + `]}`
		resp, status, _ := doRequest(t, srv, "POST", "/api/v1/coordinator-summaries", body)
		return resp, status
	}

	resp, status := post("ok", `{"number":12,"headSha":"headsha1"}`)
	var created struct {
		ReadyPRs []struct {
			Number      int    `json:"number"`
			HeadSHA     string `json:"headSha"`
			ReviewRunID string `json:"reviewRunId"`
		} `json:"readyPRs"`
	}
	if err := json.Unmarshal(resp, &created); err != nil || status != http.StatusCreated || len(created.ReadyPRs) != 1 ||
		created.ReadyPRs[0].Number != 12 || created.ReadyPRs[0].HeadSHA != "headsha1" || created.ReadyPRs[0].ReviewRunID != "run-1" {
		t.Fatalf("status=%d body=%s err=%v", status, resp, err)
	}

	for name, ready := range map[string]string{
		"stale head": `{"number":12,"headSha":"moved"}`,
		"unknown PR": `{"number":99,"headSha":"headsha1"}`,
	} {
		resp, status := post("bad-"+name, ready)
		if status != http.StatusBadRequest || !strings.Contains(string(resp), "READY_PR_INVALID") || !strings.Contains(string(resp), `"reason"`) {
			t.Fatalf("%s: status=%d body=%s, want 400 READY_PR_INVALID", name, status, resp)
		}
	}
}
