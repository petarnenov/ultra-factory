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
	"github.com/aoagents/agent-orchestrator/backend/internal/knowledgegraph"
)

// KnowledgeService records lessons and searches the knowledge graph.
type KnowledgeService interface {
	AddLesson(context.Context, knowledgegraph.LessonInput) (string, error)
	Search(context.Context, knowledgegraph.SearchInput) ([]knowledgegraph.Item, error)
}

// KnowledgeController owns /knowledge (ADR 0012). A nil Svc means no graph
// is configured on this daemon.
type KnowledgeController struct {
	Svc KnowledgeService
}

// maxCreateLessonBytes bounds one lesson request body.
const maxCreateLessonBytes = 16 << 10

// Register mounts the knowledge routes on r.
func (c *KnowledgeController) Register(r chi.Router) {
	r.Post("/knowledge/lessons", c.addLesson)
	r.Get("/knowledge/search", c.search)
}

func (c *KnowledgeController) addLesson(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "POST", "/api/v1/knowledge/lessons")
		return
	}
	var req CreateLessonRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCreateLessonBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	id, err := c.Svc.AddLesson(r.Context(), knowledgegraph.LessonInput{
		SessionID: domain.SessionID(req.SessionID), Kind: req.Kind, Text: req.Text, Topics: req.Topics,
	})
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusCreated, CreateLessonResponse{ID: id})
}

func (c *KnowledgeController) search(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/knowledge/search")
		return
	}
	q := r.URL.Query()
	limit := 0
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 50 {
			envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_LIMIT", "limit must be between 1 and 50", nil)
			return
		}
		limit = n
	}
	items, err := c.Svc.Search(r.Context(), knowledgegraph.SearchInput{
		SessionID: domain.SessionID(q.Get("sessionId")), ProjectID: domain.ProjectID(q.Get("projectId")),
		Text: q.Get("q"), Limit: limit,
	})
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	resp := KnowledgeSearchResponse{Items: make([]KnowledgeItem, len(items))}
	for i, it := range items {
		lessons := it.Lessons
		if lessons == nil {
			lessons = []string{}
		}
		resp.Items[i] = KnowledgeItem{
			Section: it.Section, ID: it.ID, Kind: it.Kind, Title: it.Title,
			Outcome: it.Outcome, Text: it.Text, Lessons: lessons, Score: it.Score,
		}
	}
	envelope.WriteJSON(w, http.StatusOK, resp)
}
