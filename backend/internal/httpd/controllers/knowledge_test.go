package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/knowledgegraph"
)

type fakeKnowledge struct {
	lessons  []knowledgegraph.LessonInput
	searches []knowledgegraph.SearchInput
	items    []knowledgegraph.Item
	merged   bool
	err      error
	ratings  []knowledgegraph.RatingInput
	retired  []string
}

func (f *fakeKnowledge) RateLesson(_ context.Context, in knowledgegraph.RatingInput) error {
	f.ratings = append(f.ratings, in)
	return f.err
}

func (f *fakeKnowledge) RetireLesson(_ context.Context, id, reason string) error {
	f.retired = append(f.retired, id+":"+reason)
	return f.err
}

func (f *fakeKnowledge) AddLesson(_ context.Context, in knowledgegraph.LessonInput) (knowledgegraph.AddedLesson, error) {
	f.lessons = append(f.lessons, in)
	if f.err != nil {
		return knowledgegraph.AddedLesson{}, f.err
	}
	return knowledgegraph.AddedLesson{ID: "lesson-1", Merged: f.merged}, nil
}

func (f *fakeKnowledge) Search(_ context.Context, in knowledgegraph.SearchInput) ([]knowledgegraph.Item, error) {
	f.searches = append(f.searches, in)
	return f.items, f.err
}

type apiErrorBody struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func doKnowledge(t *testing.T, method, rawURL, body string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, rawURL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode %s %s: %v", method, rawURL, err)
	}
	return resp, raw
}

func TestKnowledgeRoutesNotImplementedWithoutService(t *testing.T) {
	srv := newRouterServer(t, httpd.APIDeps{})
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "add lesson", method: http.MethodPost, path: "/api/v1/knowledge/lessons", body: `{"sessionId":"s1","text":"x"}`},
		{name: "search", method: http.MethodGet, path: "/api/v1/knowledge/search?q=x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, raw := doKnowledge(t, tt.method, srv.URL+tt.path, tt.body)

			var body apiErrorBody
			_ = json.Unmarshal(raw, &body)
			if resp.StatusCode != http.StatusNotImplemented || body.Code != "NOT_IMPLEMENTED" {
				t.Fatalf("status = %d body = %s", resp.StatusCode, raw)
			}
		})
	}
}

func TestKnowledgeAddLesson(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		svcErr     error
		wantStatus int
		wantCode   string
		wantCalls  int
	}{
		{name: "invalid json", body: `{`, wantStatus: http.StatusBadRequest, wantCode: "INVALID_JSON"},
		{name: "unknown field", body: `{"sessionId":"s1","text":"x","extra":1}`, wantStatus: http.StatusBadRequest, wantCode: "INVALID_JSON"},
		{
			name: "service validation error passes through", body: `{"sessionId":"s1","text":""}`,
			svcErr:     apierr.Invalid("LESSON_TEXT_REQUIRED", "lesson text is required", nil),
			wantStatus: http.StatusBadRequest, wantCode: "LESSON_TEXT_REQUIRED", wantCalls: 1,
		},
		{
			name: "graph unavailable passes through", body: `{"sessionId":"s1","text":"x"}`,
			svcErr:     apierr.Unavailable("KNOWLEDGE_GRAPH_UNAVAILABLE", "down"),
			wantStatus: http.StatusServiceUnavailable, wantCode: "KNOWLEDGE_GRAPH_UNAVAILABLE", wantCalls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeKnowledge{err: tt.svcErr}
			srv := newRouterServer(t, httpd.APIDeps{Knowledge: svc})

			resp, raw := doKnowledge(t, http.MethodPost, srv.URL+"/api/v1/knowledge/lessons", tt.body)

			var body apiErrorBody
			_ = json.Unmarshal(raw, &body)
			if resp.StatusCode != tt.wantStatus || body.Code != tt.wantCode {
				t.Fatalf("status = %d body = %s, want %d %s", resp.StatusCode, raw, tt.wantStatus, tt.wantCode)
			}
			if len(svc.lessons) != tt.wantCalls {
				t.Fatalf("service calls = %d, want %d", len(svc.lessons), tt.wantCalls)
			}
		})
	}
}

func TestKnowledgeAddLessonCreated(t *testing.T) {
	svc := &fakeKnowledge{}
	srv := newRouterServer(t, httpd.APIDeps{Knowledge: svc})

	resp, raw := doKnowledge(t, http.MethodPost, srv.URL+"/api/v1/knowledge/lessons",
		`{"sessionId":"s1","kind":"fix","text":"run make sqlc","topics":["sqlite"]}`)

	var created controllers.CreateLessonResponse
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated || created != (controllers.CreateLessonResponse{ID: "lesson-1"}) {
		t.Fatalf("status = %d body = %s", resp.StatusCode, raw)
	}
	want := knowledgegraph.LessonInput{SessionID: "s1", Kind: "fix", Text: "run make sqlc", Topics: []string{"sqlite"}}
	if len(svc.lessons) != 1 || !reflect.DeepEqual(svc.lessons[0], want) {
		t.Fatalf("lessons = %+v, want %+v", svc.lessons, want)
	}
}

func TestKnowledgeAddLessonMerged(t *testing.T) {
	srv := newRouterServer(t, httpd.APIDeps{Knowledge: &fakeKnowledge{merged: true}})

	resp, raw := doKnowledge(t, http.MethodPost, srv.URL+"/api/v1/knowledge/lessons", `{"sessionId":"s1","text":"run make sqlc"}`)

	if resp.StatusCode != http.StatusCreated || string(raw) != `{"id":"lesson-1","merged":true}` {
		t.Fatalf("status = %d body = %s", resp.StatusCode, raw)
	}
}

func TestKnowledgeRateAndRetireLesson(t *testing.T) {
	svc := &fakeKnowledge{}
	srv := newRouterServer(t, httpd.APIDeps{Knowledge: svc})
	resp, raw := doKnowledge(t, http.MethodPost, srv.URL+"/api/v1/knowledge/lessons/lesson-1/rating", `{"sessionId":"s1","rating":"wrong"}`)
	if resp.StatusCode != http.StatusOK || string(raw) != `{"rated":true}` {
		t.Fatalf("rate status=%d body=%s", resp.StatusCode, raw)
	}
	wantRating := knowledgegraph.RatingInput{SessionID: "s1", LessonID: "lesson-1", Rating: "wrong"}
	if len(svc.ratings) != 1 || svc.ratings[0] != wantRating {
		t.Fatalf("ratings = %+v", svc.ratings)
	}
	resp, raw = doKnowledge(t, http.MethodPost, srv.URL+"/api/v1/knowledge/lessons/lesson-1/retire", `{"reason":"obsolete"}`)
	if resp.StatusCode != http.StatusOK || string(raw) != `{"retired":true}` {
		t.Fatalf("retire status=%d body=%s", resp.StatusCode, raw)
	}
	if len(svc.retired) != 1 || svc.retired[0] != "lesson-1:obsolete" {
		t.Fatalf("retired = %+v", svc.retired)
	}
}

func TestKnowledgeSearchInvalidLimit(t *testing.T) {
	for _, limit := range []string{"0", "51", "abc"} {
		t.Run(limit, func(t *testing.T) {
			svc := &fakeKnowledge{}
			srv := newRouterServer(t, httpd.APIDeps{Knowledge: svc})

			resp, raw := doKnowledge(t, http.MethodGet, srv.URL+"/api/v1/knowledge/search?q=x&limit="+limit, "")

			var body apiErrorBody
			_ = json.Unmarshal(raw, &body)
			if resp.StatusCode != http.StatusBadRequest || body.Code != "INVALID_LIMIT" || len(svc.searches) != 0 {
				t.Fatalf("status = %d body = %s calls = %d", resp.StatusCode, raw, len(svc.searches))
			}
		})
	}
}

func TestKnowledgeSearch(t *testing.T) {
	svc := &fakeKnowledge{items: []knowledgegraph.Item{
		{Section: "lesson", ID: "l1", Kind: "fix", Title: "src", Outcome: "merged", Text: "t", Score: 2.5},
		{Section: "attempt", ID: "h/s0", Title: "old", Outcome: "closed", Lessons: []string{"use -race"}, Score: 1},
	}}
	srv := newRouterServer(t, httpd.APIDeps{Knowledge: svc})
	q := url.Values{"q": {"flaky sqlite"}, "sessionId": {"s1"}, "projectId": {"ao"}, "limit": {"7"}}

	resp, raw := doKnowledge(t, http.MethodGet, srv.URL+"/api/v1/knowledge/search?"+q.Encode(), "")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body = %s", resp.StatusCode, raw)
	}
	var got controllers.KnowledgeSearchResponse
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	want := controllers.KnowledgeSearchResponse{Items: []controllers.KnowledgeItem{
		{Section: "lesson", ID: "l1", Kind: "fix", Title: "src", Outcome: "merged", Text: "t", Lessons: []string{}, Score: 2.5},
		{Section: "attempt", ID: "h/s0", Title: "old", Outcome: "closed", Lessons: []string{"use -race"}, Score: 1},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("response = %+v, want %+v", got, want)
	}
	if !strings.Contains(string(raw), `"lessons":[]`) {
		t.Fatalf("nil lessons must serialize as []: %s", raw)
	}
	wantIn := knowledgegraph.SearchInput{SessionID: "s1", ProjectID: "ao", Text: "flaky sqlite", Limit: 7}
	if len(svc.searches) != 1 || svc.searches[0] != wantIn {
		t.Fatalf("search input = %+v, want %+v", svc.searches, wantIn)
	}
}

func TestKnowledgeSearchDefaultsAndErrors(t *testing.T) {
	tests := []struct {
		name       string
		items      []knowledgegraph.Item
		svcErr     error
		wantStatus int
		wantCode   string
		wantBody   string
	}{
		{name: "no items is an empty list", wantStatus: http.StatusOK, wantBody: `{"items":[]}`},
		{
			name: "service error envelope passes through", svcErr: apierr.Invalid("PROJECT_REQUIRED", "projectId or sessionId is required", nil),
			wantStatus: http.StatusBadRequest, wantCode: "PROJECT_REQUIRED",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeKnowledge{items: tt.items, err: tt.svcErr}
			srv := newRouterServer(t, httpd.APIDeps{Knowledge: svc})

			resp, raw := doKnowledge(t, http.MethodGet, srv.URL+"/api/v1/knowledge/search?q=flaky", "")

			var body apiErrorBody
			_ = json.Unmarshal(raw, &body)
			if resp.StatusCode != tt.wantStatus || body.Code != tt.wantCode {
				t.Fatalf("status = %d body = %s", resp.StatusCode, raw)
			}
			if tt.wantBody != "" && string(raw) != tt.wantBody {
				t.Fatalf("body = %s, want %s", raw, tt.wantBody)
			}
			if svc.searches[0].Limit != 0 {
				t.Fatalf("limit = %d, want 0 default", svc.searches[0].Limit)
			}
		})
	}
}
