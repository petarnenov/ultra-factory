package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
)

// ReportService is the controller-facing report creation contract.
type ReportService interface {
	Create(context.Context, reportsvc.CreateInput) (domain.ReportRecord, error)
}

type reportProjectReader interface {
	ListProject(context.Context, domain.ProjectID) ([]domain.ReportRecord, error)
}

type reportProjectPager interface {
	ListProjectPage(context.Context, domain.ProjectID, reportsvc.ListProjectFilter) (reportsvc.ListProjectPage, error)
}

// reportPuller serves a peer daemon that delivers this daemon's worker
// reports to its own orchestrator.
type reportPuller interface {
	ListDuePending(context.Context, domain.SessionID) ([]domain.ReportRecord, bool, error)
	AcknowledgePulled(context.Context, domain.SessionID, []string) ([]string, error)
}

// MaxCreateReportBytes bounds one report creation request.
const MaxCreateReportBytes = 256 << 10

// ReportsController owns the /reports routes.
type ReportsController struct{ Svc ReportService }

// Register mounts report routes on r.
func (c *ReportsController) Register(r chi.Router) {
	r.Post("/reports", c.create)
	r.Get("/reports", c.list)
	r.Get("/reports/pending", c.pending)
	r.Post("/reports/acknowledge", c.acknowledge)
}

func (c *ReportsController) list(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/reports")
		return
	}
	reader, _ := c.Svc.(reportProjectReader)
	pager, isPager := c.Svc.(reportProjectPager)
	if !isPager && reader == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/reports")
		return
	}
	_ = reader
	_ = pager
	projectID := domain.ProjectID(r.URL.Query().Get("projectId"))
	if projectID == "" {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "validation", "REPORT_PROJECT_REQUIRED", "Project id is required", nil)
		return
	}
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			envelope.WriteAPIError(w, r, http.StatusBadRequest, "validation", "INVALID_REPORT_LIMIT", "limit must be a positive integer", nil)
			return
		}
		limit = parsed
	}
	if pager, ok := c.Svc.(reportProjectPager); ok {
		page, err := pager.ListProjectPage(r.Context(), projectID, reportsvc.ListProjectFilter{
			Limit:  limit,
			Cursor: r.URL.Query().Get("cursor"),
		})
		if err != nil {
			envelope.WriteError(w, r, err)
			return
		}
		response := listReportsResponse(page.Reports)
		response.NextCursor = page.NextCursor
		envelope.WriteJSON(w, http.StatusOK, response)
		return
	}
	if reader == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/reports")
		return
	}
	records, err := reader.ListProject(r.Context(), projectID)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, listReportsResponse(records))
}

func listReportsResponse(records []domain.ReportRecord) ListReportsResponse {
	response := ListReportsResponse{Reports: make([]ReportResponse, len(records))}
	for i, record := range records {
		outputs := make([]ReportOutputResponse, len(record.Outputs))
		for j, output := range record.Outputs {
			outputs[j] = ReportOutputResponse{Kind: string(output.Kind), Reference: output.Reference, Label: output.Label}
		}
		response.Reports[i] = ReportResponse{
			ID: record.ID, SessionID: string(record.SessionID), ProjectID: string(record.ProjectID),
			State: string(record.State), Note: record.Note, Message: record.Message,
			Outputs: outputs, CreatedAt: record.CreatedAt, RepeatCount: record.RepeatCount,
			HostID: record.HostID, HostLabel: record.HostLabel, Agent: record.Agent, SessionName: record.SessionDisplayName,
			SourceProjectID: func() string {
				if record.SourceProjectID != "" {
					return record.SourceProjectID
				}
				return string(record.ProjectID)
			}(),
		}
	}
	return response
}

func (c *ReportsController) pending(w http.ResponseWriter, r *http.Request) {
	puller, ok := c.Svc.(reportPuller)
	if c.Svc == nil || !ok {
		apispec.NotImplemented(w, r, "GET", "/api/v1/reports/pending")
		return
	}
	sessionID := domain.SessionID(r.URL.Query().Get("sessionId"))
	if sessionID == "" {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "validation", "INVALID_REPORT_SESSION", "Worker session id is required", nil)
		return
	}
	records, morePending, err := puller.ListDuePending(r.Context(), sessionID)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, ListPendingReportsResponse{Reports: listReportsResponse(records).Reports, MorePending: morePending})
}

func (c *ReportsController) acknowledge(w http.ResponseWriter, r *http.Request) {
	puller, ok := c.Svc.(reportPuller)
	if c.Svc == nil || !ok {
		apispec.NotImplemented(w, r, "POST", "/api/v1/reports/acknowledge")
		return
	}
	var req AcknowledgeReportsRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	acknowledged, err := puller.AcknowledgePulled(r.Context(), domain.SessionID(req.SessionID), req.ReportIDs)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, AcknowledgeReportsResponse{Acknowledged: acknowledged})
}

func (c *ReportsController) create(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "POST", "/api/v1/reports")
		return
	}
	var req CreateReportRequest
	// A report is a short claim plus references; the bound also keeps any one
	// report far below what a peer daemon pulling it will read.
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxCreateReportBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	outputs := make([]domain.ReportOutput, len(req.Outputs))
	for i, output := range req.Outputs {
		outputs[i] = domain.ReportOutput{Kind: domain.ReportOutputKind(output.Kind), Reference: output.Reference, Label: output.Label}
	}
	rec, err := c.Svc.Create(r.Context(), reportsvc.CreateInput{
		SessionID: domain.SessionID(req.SessionID), State: domain.ReportState(req.State),
		Note: req.Note, Message: req.Message, Outputs: outputs,
	})
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusCreated, CreateReportResponse{ID: rec.ID})
}
