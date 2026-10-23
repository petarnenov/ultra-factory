package controllers

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/aoagents/agent-orchestrator/backend/internal/consultations"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	"github.com/go-chi/chi/v5"
)

type ConsultationService interface {
	List(context.Context, int, string) (consultations.Page, error)
	Detail(context.Context, string) (consultations.Detail, error)
}

type ConsultationController struct{ Svc ConsultationService }

func (c *ConsultationController) Register(r chi.Router) {
	r.Get("/knowledge/consultations", c.list)
	r.Get("/knowledge/consultations/{id}", c.detail)
}

func (c *ConsultationController) list(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/knowledge/consultations")
		return
	}
	limit := 25
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 50 {
			consultationError(w, r, consultations.ErrInvalid)
			return
		}
		limit = n
	}
	page, err := c.Svc.List(r.Context(), limit, r.URL.Query().Get("cursor"))
	if err != nil {
		consultationError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, page)
}

func (c *ConsultationController) detail(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/knowledge/consultations/{id}")
		return
	}
	detail, err := c.Svc.Detail(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		consultationError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, detail)
}

func consultationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, consultations.ErrInvalid):
		envelope.WriteAPIError(w, r, 400, "bad_request", "INVALID_CONSULTATION_QUERY", "Invalid consultation identifier, cursor or limit", nil)
	case errors.Is(err, consultations.ErrMissing):
		envelope.WriteAPIError(w, r, 404, "not_found", "CONSULTATION_NOT_FOUND", "Consultation not found", nil)
	default:
		envelope.WriteAPIError(w, r, 503, "unavailable", "KNOWLEDGE_GRAPH_UNAVAILABLE", "Consultation index unavailable; retry later", nil)
	}
}
