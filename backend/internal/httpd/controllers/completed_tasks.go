package controllers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/taskreport"
)

// CompletedTaskService reads a project's completed-task reports.
type CompletedTaskService interface {
	List(context.Context, domain.ProjectID, taskreport.ListInput) (taskreport.Page, error)
	Get(context.Context, domain.ProjectID, domain.SessionID) (taskreport.Report, error)
}

// CompletedTasksController owns /projects/{projectId}/completed-tasks.
type CompletedTasksController struct {
	Svc    CompletedTaskService
	HostID string
}

// Register mounts the completed-task routes on r.
func (c *CompletedTasksController) Register(r chi.Router) {
	r.Get("/projects/{projectId}/completed-tasks", c.list)
	r.Get("/projects/{projectId}/completed-tasks/{sessionId}", c.get)
}

func (c *CompletedTasksController) list(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/projects/{projectId}/completed-tasks")
		return
	}
	q := r.URL.Query()
	limit := 0
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > taskreport.MaxLimit {
			envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_LIMIT", "limit must be between 1 and 200", nil)
			return
		}
		limit = n
	}
	page, err := c.Svc.List(r.Context(), domain.ProjectID(chi.URLParam(r, "projectId")), taskreport.ListInput{
		Outcome: taskreport.Outcome(q.Get("outcome")), Limit: limit, Cursor: q.Get("cursor"),
	})
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	resp := ListCompletedTasksResponse{HostID: c.HostID, Tasks: make([]CompletedTaskSummary, len(page.Tasks)), NextCursor: page.NextCursor}
	for i, task := range page.Tasks {
		resp.Tasks[i] = c.summary(task, resp.HostID)
	}
	envelope.WriteJSON(w, http.StatusOK, resp)
}

func (c *CompletedTasksController) get(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/projects/{projectId}/completed-tasks/{sessionId}")
		return
	}
	report, err := c.Svc.Get(r.Context(), domain.ProjectID(chi.URLParam(r, "projectId")), domain.SessionID(chi.URLParam(r, "sessionId")))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	resp := CompletedTaskReportResponse{
		CompletedTaskSummary: c.summary(report.Summary, c.HostID),
		Config: CompletedTaskConfigHistory{
			Completeness: string(report.Config.Completeness),
			Segments:     make([]CompletedTaskConfigSegment, len(report.Config.Segments)),
		},
		Runs: make([]CompletedTaskRun, len(report.Runs)),
		Stages: CompletedTaskStages{
			Completeness: string(report.Stages.Completeness),
			Visits:       make([]CompletedTaskStageVisit, len(report.Stages.Visits)),
			Totals:       make([]CompletedTaskStageTotal, len(report.Stages.Totals)),
		},
	}
	for i, seg := range report.Config.Segments {
		resp.Config.Segments[i] = CompletedTaskConfigSegment{
			Source: string(seg.Source), Harness: seg.Harness, Model: seg.Model, Effort: seg.Effort, Since: seg.Since,
		}
	}
	for i, run := range report.Runs {
		resp.Runs[i] = CompletedTaskRun{
			BindingID: run.BindingID, Harness: string(run.Harness), NativeRootID: run.NativeRootID,
			Model: run.Model, EventCount: run.EventCount, InputTokens: run.InputTokens,
			CachedInputTokens: run.CachedInputTokens, OutputTokens: run.OutputTokens,
			Completeness: string(run.Completeness),
		}
	}
	for i, visit := range report.Stages.Visits {
		resp.Stages.Visits[i] = CompletedTaskStageVisit{
			Stage: string(visit.Stage), EnteredAt: visit.EnteredAt, LeftAt: visit.LeftAt,
			DurationSeconds: visit.DurationSeconds,
		}
	}
	for i, total := range report.Stages.Totals {
		resp.Stages.Totals[i] = CompletedTaskStageTotal{
			Stage: string(total.Stage), Seconds: total.Seconds, Visits: total.Visits, HasUnfinished: total.HasUnfinished,
		}
	}
	envelope.WriteJSON(w, http.StatusOK, resp)
}

func (c *CompletedTasksController) summary(task taskreport.Summary, hostID string) CompletedTaskSummary {
	out := CompletedTaskSummary{
		HostID: hostID, SessionID: string(task.SessionID), ProjectID: string(task.ProjectID),
		Title: task.Title, IssueID: string(task.IssueID), PRs: make([]CompletedTaskPR, len(task.PRs)),
		Outcome: string(task.Outcome), CreatedAt: task.CreatedAt, CompletedAt: task.CompletedAt,
		ElapsedSeconds: task.ElapsedSeconds, Harness: string(task.Harness), Model: task.Model, Effort: task.Effort,
		Tokens: CompletedTaskTokens{
			InputTokens: task.Tokens.InputTokens, CachedInputTokens: task.Tokens.CachedInputTokens,
			OutputTokens: task.Tokens.OutputTokens, Completeness: string(task.Tokens.Completeness),
		},
	}
	for i, pr := range task.PRs {
		out.PRs[i] = CompletedTaskPR{
			URL: pr.URL, Number: pr.Number, State: string(pr.State), Title: pr.Title,
			Additions: pr.Additions, Deletions: pr.Deletions, ChangedFiles: pr.ChangedFiles,
		}
	}
	return out
}
