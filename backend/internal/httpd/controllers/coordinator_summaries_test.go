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

func TestCoordinatorSummaryRouteRejectsReservedSubmissionKey(t *testing.T) {
	_, coordinator, _, _, deps := summaryRouteFixture(t)
	srv := newRouterServer(t, *deps)
	body := `{"coordinatorSessionId":"` + string(coordinator.ID) + `","submissionKey":"daemon:coordinator_terminated:1","state":"working"}`
	resp, status, _ := doRequest(t, srv, "POST", "/api/v1/coordinator-summaries", body)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", status, resp)
	}
}
