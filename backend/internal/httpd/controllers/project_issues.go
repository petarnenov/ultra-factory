package controllers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
)

// ProjectIssueService reads one issue from a project's tracker.
type ProjectIssueService interface {
	ProjectIssue(ctx context.Context, projectID domain.ProjectID, number int) (domain.Issue, error)
}

// getProjectIssue returns one issue of the project's tracker.
func (c *SessionsController) getProjectIssue(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.Svc.(ProjectIssueService)
	if !ok {
		apispec.NotImplemented(w, r, "GET", "/api/v1/projects/{id}/issues/{number}")
		return
	}
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil || number < 1 {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_ISSUE_NUMBER", "Issue number must be a positive integer", nil)
		return
	}
	issue, err := svc.ProjectIssue(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), number)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, ProjectIssueResponse{
		Number: number,
		Title:  issue.Title,
		Body:   issue.Body,
		URL:    issue.URL,
	})
}
