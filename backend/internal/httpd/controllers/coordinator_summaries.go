package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
)

// CoordinatorSummaryService is the controller-facing summary contract.
type CoordinatorSummaryService interface {
	Submit(context.Context, reportsvc.SummaryInput) (domain.CoordinatorSummary, bool, error)
	List(context.Context, domain.SessionID, int) ([]domain.CoordinatorSummary, error)
}

// MaxCoordinatorSummaryBytes bounds one submission request.
const MaxCoordinatorSummaryBytes = 32 << 10

// CoordinatorSummaryRequest is a coordinator's summary or escalation. The
// recipient is always the project root, so there is no recipient field.
type CoordinatorSummaryRequest struct {
	CoordinatorSessionID string   `json:"coordinatorSessionId"`
	SubmissionKey        string   `json:"submissionKey" minLength:"1" maxLength:"200"`
	Scope                string   `json:"scope,omitempty" maxLength:"200"`
	State                string   `json:"state" enum:"planning,working,blocked,done"`
	WorkerSessionIDs     []string `json:"workerSessionIds,omitempty" maxItems:"3" description:"Owned worker session ids; qualify a remote worker as host:id when needed."`
	// ReadyPRs hands PRs to the root as ready to merge. The daemon validates
	// ownership, the current head, and an approved review run per entry.
	ReadyPRs         []ReadyPRRequest `json:"readyPRs,omitempty" maxItems:"10"`
	Result           string           `json:"result,omitempty" maxLength:"2000"`
	EscalationReason string           `json:"escalationReason,omitempty" enum:"overlapping_files,unclear_acceptance,worker_failed,scope_decision,other"`
	EscalationNote   string           `json:"escalationNote,omitempty" maxLength:"500"`
}

// ReadyPRRequest names a PR head a coordinator declares ready to merge.
type ReadyPRRequest struct {
	Number  int    `json:"number" minimum:"1"`
	HeadSHA string `json:"headSha" minLength:"1" maxLength:"64"`
	// ReviewRunID is optional; omitted resolves to the newest approved run.
	ReviewRunID string `json:"reviewRunId,omitempty" maxLength:"200"`
}

// ReadyPRResponse is a stored ready PR with its approved review run.
type ReadyPRResponse struct {
	Number      int    `json:"number"`
	HeadSHA     string `json:"headSha"`
	ReviewRunID string `json:"reviewRunId"`
}

// SummaryRemoteWorkerResponse describes one linked worker in a summary.
type SummaryRemoteWorkerResponse struct {
	HostID          string `json:"hostId"`
	HostLabel       string `json:"hostLabel"`
	RemoteSessionID string `json:"remoteSessionId"`
}

// CoordinatorSummaryResponse is one persisted coordinator summary and its
// delivery state. It is distinct from a worker report.
type CoordinatorSummaryResponse struct {
	ID                   string                        `json:"id"`
	CoordinatorSessionID string                        `json:"coordinatorSessionId"`
	ProjectID            string                        `json:"projectId"`
	SubmissionKey        string                        `json:"submissionKey"`
	Scope                string                        `json:"scope"`
	State                string                        `json:"state" enum:"planning,working,blocked,done"`
	WorkerSessionIDs     []string                      `json:"workerSessionIds"`
	RemoteWorkers        []SummaryRemoteWorkerResponse `json:"remoteWorkers"`
	ReadyPRs             []ReadyPRResponse             `json:"readyPRs"`
	Result               string                        `json:"result"`
	EscalationReason     string                        `json:"escalationReason,omitempty"`
	EscalationNote       string                        `json:"escalationNote,omitempty"`
	Escalation           bool                          `json:"escalation"`
	CreatedAt            time.Time                     `json:"createdAt"`
	DeliveryState        string                        `json:"deliveryState" enum:"pending,claimed,acknowledged"`
	DeliveryAttempts     int64                         `json:"deliveryAttempts"`
	AcknowledgedAt       *time.Time                    `json:"acknowledgedAt,omitempty"`
	LastError            string                        `json:"lastError,omitempty"`
}

// CoordinatorSummaryHistoryResponse lists summaries newest first.
type CoordinatorSummaryHistoryResponse struct {
	Summaries []CoordinatorSummaryResponse `json:"summaries"`
}

// CoordinatorSummaryHistoryQuery bounds one history read.
type CoordinatorSummaryHistoryQuery struct {
	Limit int `query:"limit,omitempty" minimum:"1" maximum:"100" description:"Maximum summaries to return, newest first."`
}

// CoordinatorSummariesController owns the coordinator summary routes.
type CoordinatorSummariesController struct{ Svc CoordinatorSummaryService }

// Register mounts coordinator summary routes on r.
func (c *CoordinatorSummariesController) Register(r chi.Router) {
	r.Post("/coordinator-summaries", c.submit)
	r.Get("/sessions/{sessionId}/summaries", c.history)
}

func (c *CoordinatorSummariesController) submit(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "POST", "/api/v1/coordinator-summaries")
		return
	}
	var req CoordinatorSummaryRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxCoordinatorSummaryBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	if domain.EscalationReason(req.EscalationReason) == domain.EscalationCoordinatorTerminated {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_SUMMARY", "The coordinator_terminated reason is reserved for the daemon", nil)
		return
	}
	workers := make([]domain.SessionID, len(req.WorkerSessionIDs))
	for i, id := range req.WorkerSessionIDs {
		workers[i] = domain.SessionID(id)
	}
	ready := make([]domain.ReadyPR, len(req.ReadyPRs))
	for i, pr := range req.ReadyPRs {
		ready[i] = domain.ReadyPR{Number: pr.Number, HeadSHA: pr.HeadSHA, ReviewRunID: pr.ReviewRunID}
	}
	rec, created, err := c.Svc.Submit(r.Context(), reportsvc.SummaryInput{
		CoordinatorSessionID: domain.SessionID(req.CoordinatorSessionID), SubmissionKey: req.SubmissionKey,
		Scope: req.Scope, State: domain.CoordinatorSummaryState(req.State), WorkerSessionIDs: workers, ReadyPRs: ready,
		Result: req.Result, EscalationReason: domain.EscalationReason(req.EscalationReason), EscalationNote: req.EscalationNote,
	})
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	envelope.WriteJSON(w, status, coordinatorSummaryResponse(rec))
}

func (c *CoordinatorSummariesController) history(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/sessions/{sessionId}/summaries")
		return
	}
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			envelope.WriteAPIError(w, r, http.StatusBadRequest, "validation", "INVALID_SUMMARY_LIMIT", "limit must be a positive integer", nil)
			return
		}
		limit = parsed
	}
	recs, err := c.Svc.List(r.Context(), domain.SessionID(chi.URLParam(r, "sessionId")), limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	out := CoordinatorSummaryHistoryResponse{Summaries: make([]CoordinatorSummaryResponse, len(recs))}
	for i, rec := range recs {
		out.Summaries[i] = coordinatorSummaryResponse(rec)
	}
	envelope.WriteJSON(w, http.StatusOK, out)
}

func coordinatorSummaryResponse(rec domain.CoordinatorSummary) CoordinatorSummaryResponse {
	workers := make([]string, len(rec.WorkerSessionIDs))
	for i, id := range rec.WorkerSessionIDs {
		workers[i] = string(id)
	}
	remoteWorkers := make([]SummaryRemoteWorkerResponse, len(rec.RemoteWorkers))
	for i, worker := range rec.RemoteWorkers {
		remoteWorkers[i] = SummaryRemoteWorkerResponse{
			HostID: worker.HostID, HostLabel: worker.HostLabel, RemoteSessionID: worker.RemoteSessionID,
		}
	}
	readyPRs := make([]ReadyPRResponse, len(rec.ReadyPRs))
	for i, pr := range rec.ReadyPRs {
		readyPRs[i] = ReadyPRResponse{Number: pr.Number, HeadSHA: pr.HeadSHA, ReviewRunID: pr.ReviewRunID}
	}
	out := CoordinatorSummaryResponse{
		ID: rec.ID, CoordinatorSessionID: string(rec.CoordinatorSessionID), ProjectID: string(rec.ProjectID),
		SubmissionKey: rec.SubmissionKey, Scope: rec.Scope, State: string(rec.State), WorkerSessionIDs: workers,
		RemoteWorkers: remoteWorkers,
		Result:        rec.Result, EscalationReason: string(rec.EscalationReason), EscalationNote: rec.EscalationNote,
		Escalation: rec.IsEscalation(), CreatedAt: rec.CreatedAt, DeliveryState: string(rec.DeliveryState),
		DeliveryAttempts: rec.DeliveryAttempts, LastError: rec.LastError, ReadyPRs: readyPRs,
	}
	if !rec.AcknowledgedAt.IsZero() {
		at := rec.AcknowledgedAt
		out.AcknowledgedAt = &at
	}
	return out
}
