package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/taskreport"
)

type fakeCompletedTasks struct {
	lastInput taskreport.ListInput
}

func (f *fakeCompletedTasks) List(_ context.Context, project domain.ProjectID, in taskreport.ListInput) (taskreport.Page, error) {
	f.lastInput = in
	if project != "ao" {
		return taskreport.Page{}, apierr.NotFound("PROJECT_NOT_FOUND", "Unknown project")
	}
	input := int64(120)
	return taskreport.Page{Tasks: []taskreport.Summary{{
		SessionID: "ao-1", ProjectID: "ao", Title: "Add reports", Outcome: taskreport.OutcomeMerged,
		CreatedAt: time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC), Harness: domain.HarnessClaudeCode,
		Model: "claude-opus-5-5", Effort: "medium",
		PRs:    []taskreport.PRLink{{URL: "https://github.com/o/r/pull/1", Number: 1, State: domain.PRStateMerged}},
		Tokens: taskreport.TokenTotals{InputTokens: &input, Completeness: taskreport.Partial},
	}}, NextCursor: "1"}, nil
}

func (f *fakeCompletedTasks) Get(_ context.Context, _ domain.ProjectID, id domain.SessionID) (taskreport.Report, error) {
	if id != "ao-1" {
		return taskreport.Report{}, apierr.NotFound("COMPLETED_TASK_NOT_FOUND", "No completed task")
	}
	return taskreport.Report{
		Summary: taskreport.Summary{SessionID: "ao-1", ProjectID: "ao", Outcome: taskreport.OutcomeCancelled,
			Tokens: taskreport.TokenTotals{Completeness: taskreport.Unavailable}},
		Config: taskreport.ConfigHistory{Completeness: taskreport.Unavailable},
		Stages: taskreport.StageHistory{Completeness: taskreport.Complete,
			Visits: []taskreport.StageVisit{{Stage: domain.KanbanBuilding, DurationSeconds: 30}},
			Totals: []taskreport.StageTotal{{Stage: domain.KanbanBuilding, Seconds: 30, Visits: 1}}},
	}, nil
}

func TestCompletedTaskRoutes(t *testing.T) {
	svc := &fakeCompletedTasks{}
	srv := newRouterServer(t, httpd.APIDeps{CompletedTasks: svc, HostID: "host-linux"})

	resp, err := http.Get(srv.URL + "/api/v1/projects/ao/completed-tasks?outcome=merged&limit=10&cursor=5")
	if err != nil {
		t.Fatal(err)
	}
	var list controllers.ListCompletedTasksResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || list.HostID != "host-linux" || list.NextCursor != "1" || len(list.Tasks) != 1 {
		t.Fatalf("list = %d %+v", resp.StatusCode, list)
	}
	task := list.Tasks[0]
	if task.HostID != "host-linux" || task.Tokens.Completeness != "partial" || *task.Tokens.InputTokens != 120 ||
		task.Tokens.OutputTokens != nil || task.PRs[0].State != "merged" || task.CompletedAt != nil {
		t.Fatalf("task = %+v", task)
	}
	if svc.lastInput != (taskreport.ListInput{Outcome: "merged", Limit: 10, Cursor: "5"}) {
		t.Fatalf("input = %+v", svc.lastInput)
	}

	for path, want := range map[string]int{
		"/api/v1/projects/ao/completed-tasks?limit=0":   http.StatusBadRequest,
		"/api/v1/projects/ao/completed-tasks?limit=201": http.StatusBadRequest,
		"/api/v1/projects/nope/completed-tasks":         http.StatusNotFound,
		"/api/v1/projects/ao/completed-tasks/ao-9":      http.StatusNotFound,
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s: status = %d, want %d", path, resp.StatusCode, want)
		}
	}

	resp, err = http.Get(srv.URL + "/api/v1/projects/ao/completed-tasks/ao-1")
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	tokens := raw["tokens"].(map[string]any)
	if resp.StatusCode != http.StatusOK || raw["hostId"] != "host-linux" || tokens["inputTokens"] != nil ||
		tokens["completeness"] != "unavailable" || raw["completedAt"] != nil {
		t.Fatalf("report = %d %v", resp.StatusCode, raw)
	}
	if _, present := tokens["inputTokens"]; !present {
		t.Fatal("unavailable token counts must be explicit nulls")
	}
	stages := raw["stages"].(map[string]any)
	if stages["completeness"] != "complete" || len(stages["totals"].([]any)) != 1 {
		t.Fatalf("stages = %v", stages)
	}
}

func TestCompletedTaskRoutesNotImplementedWithoutService(t *testing.T) {
	srv := newRouterServer(t, httpd.APIDeps{})
	resp, err := http.Get(srv.URL + "/api/v1/projects/ao/completed-tasks")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
