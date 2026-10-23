package knowledgegraph

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/taskreport"
)

type fakeSessions struct {
	mu       sync.Mutex
	sessions map[domain.SessionID]domain.Session
	err      error
}

func (f *fakeSessions) Get(_ context.Context, id domain.SessionID) (domain.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return domain.Session{}, f.err
	}
	s, ok := f.sessions[id]
	if !ok {
		return domain.Session{}, apierr.NotFound("SESSION_NOT_FOUND", "no session")
	}
	return s, nil
}

type fakeTasks struct {
	reports map[domain.SessionID]taskreport.Report
}

func (f *fakeTasks) Get(_ context.Context, _ domain.ProjectID, id domain.SessionID) (taskreport.Report, error) {
	r, ok := f.reports[id]
	if !ok {
		return taskreport.Report{}, apierr.NotFound("TASK_NOT_FOUND", "not completed")
	}
	return r, nil
}

func worker(id domain.SessionID, project domain.ProjectID) domain.Session {
	var s domain.Session
	s.ID = id
	s.ProjectID = project
	s.Kind = domain.KindWorker
	return s
}

func newTestService(r Runner, sessions *fakeSessions, tasks TaskReports) *Service {
	return New(Deps{
		Graph: NewGraph(r), Sessions: sessions, Tasks: tasks, HostID: "host1",
		Now: func() time.Time { return testAt }, NewID: func() string { return "lesson-1" },
	})
}

func apiCode(t *testing.T, err error) (apierr.Kind, string) {
	t.Helper()
	var ae *apierr.Error
	if !errors.As(err, &ae) {
		t.Fatalf("error %v is not an apierr.Error", err)
	}
	return ae.Kind, ae.Code
}

func TestAddLessonValidation(t *testing.T) {
	sixTopics := []string{"a1", "a2", "a3", "a4", "a5", "a6"}
	tests := []struct {
		name string
		in   LessonInput
		code string
	}{
		{name: "session required", in: LessonInput{Text: "x"}, code: "SESSION_ID_REQUIRED"},
		{name: "text required", in: LessonInput{SessionID: "s1", Text: " \x01 "}, code: "LESSON_TEXT_REQUIRED"},
		{name: "text too long", in: LessonInput{SessionID: "s1", Text: strings.Repeat("ж", 1001)}, code: "LESSON_TEXT_TOO_LONG"},
		{name: "bad kind", in: LessonInput{SessionID: "s1", Text: "x", Kind: "rumor"}, code: "INVALID_LESSON_KIND"},
		{name: "topic too long", in: LessonInput{SessionID: "s1", Text: "x", Topics: []string{strings.Repeat("t", 41)}}, code: "INVALID_TOPIC"},
		{name: "too many topics", in: LessonInput{SessionID: "s1", Text: "x", Topics: sixTopics}, code: "TOO_MANY_TOPICS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeRunner{}
			svc := newTestService(r, &fakeSessions{sessions: map[domain.SessionID]domain.Session{"s1": worker("s1", "ao")}}, nil)

			_, err := svc.AddLesson(context.Background(), tt.in)

			kind, code := apiCode(t, err)
			if kind != apierr.KindInvalid || code != tt.code {
				t.Fatalf("error kind=%v code=%q, want invalid %q", kind, code, tt.code)
			}
			if len(r.snapshot()) != 0 {
				t.Fatalf("graph must not be called on invalid input")
			}
		})
	}
}

func TestAddLessonStoresLesson(t *testing.T) {
	r := &fakeRunner{}
	svc := newTestService(r, &fakeSessions{sessions: map[domain.SessionID]domain.Session{"s1": worker("s1", "ao")}}, nil)

	added, err := svc.AddLesson(context.Background(), LessonInput{
		SessionID: "s1", Kind: " FIX ", Text: "  Run make sqlc\x00 ",
		Topics: []string{"SQLite", "sqlite", " ", "ci"},
	})

	if err != nil {
		t.Fatalf("AddLesson() error = %v", err)
	}
	if added != (AddedLesson{ID: "lesson-1"}) {
		t.Fatalf("added = %+v, want new NewID lesson", added)
	}
	p := r.snapshot()[0].params
	want := map[string]any{
		"taskKey": "host1/s1", "sessionId": "s1", "host": "host1", "project": "ao",
		"id": "lesson-1", "kind": "fix", "text": "Run make sqlc",
		"topics": []string{"sqlite", "ci"}, "at": "2026-10-23T11:00:00Z",
	}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("params = %#v, want %#v", p, want)
	}
}

func TestAddLessonMatchingOnlyRetiredCandidateStoresNew(t *testing.T) {
	r := &fakeRunner{reply: func(statement string) ([]Row, error) {
		if statement == similarLessonsCypher {
			return nil, nil
		}
		return nil, nil
	}}
	svc := New(Deps{Graph: NewGraph(r), Sessions: &fakeSessions{sessions: map[domain.SessionID]domain.Session{"s1": worker("s1", "ao")}}, HostID: "host1", NewID: func() string { return "new-lesson" }, Now: func() time.Time { return testAt }, Embedder: &fakeEmbedder{vec: []float64{0.5}}, MergeScore: 0.9})
	added, err := svc.AddLesson(context.Background(), LessonInput{SessionID: "s1", Kind: "tip", Text: "new wording"})
	if err != nil || added != (AddedLesson{ID: "new-lesson"}) {
		t.Fatalf("AddLesson = %+v, %v", added, err)
	}
	calls := r.snapshot()
	if len(calls) < 2 || calls[0].statement != similarLessonsCypher || calls[1].statement != addLessonCypher {
		t.Fatalf("queries = %+v", calls)
	}
}

func TestAddLessonDefaultsKindToTip(t *testing.T) {
	r := &fakeRunner{}
	svc := newTestService(r, &fakeSessions{sessions: map[domain.SessionID]domain.Session{"s1": worker("s1", "ao")}}, nil)

	if _, err := svc.AddLesson(context.Background(), LessonInput{SessionID: "s1", Text: "x"}); err != nil {
		t.Fatalf("AddLesson() error = %v", err)
	}

	if got := r.snapshot()[0].params["kind"]; got != "tip" {
		t.Fatalf("kind = %v, want tip", got)
	}
}

func TestAddLessonErrors(t *testing.T) {
	tests := []struct {
		name     string
		sessions *fakeSessions
		runErr   error
		wantKind apierr.Kind
		wantCode string
	}{
		{name: "unknown session passes through", sessions: &fakeSessions{sessions: map[domain.SessionID]domain.Session{}}, wantKind: apierr.KindNotFound, wantCode: "SESSION_NOT_FOUND"},
		{name: "graph failure is unavailable", sessions: &fakeSessions{sessions: map[domain.SessionID]domain.Session{"s1": worker("s1", "ao")}}, runErr: errors.New("refused"), wantKind: apierr.KindUnavailable, wantCode: "KNOWLEDGE_GRAPH_UNAVAILABLE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(&fakeRunner{err: tt.runErr}, tt.sessions, nil)

			_, err := svc.AddLesson(context.Background(), LessonInput{SessionID: "s1", Text: "x"})

			kind, code := apiCode(t, err)
			if kind != tt.wantKind || code != tt.wantCode {
				t.Fatalf("error kind=%v code=%q, want %v %q", kind, code, tt.wantKind, tt.wantCode)
			}
		})
	}
}

func TestSearchValidation(t *testing.T) {
	tests := []struct {
		name string
		in   SearchInput
		code string
	}{
		{name: "query required", in: SearchInput{ProjectID: "ao", Text: "  "}, code: "QUERY_REQUIRED"},
		{name: "negative limit", in: SearchInput{ProjectID: "ao", Text: "flaky", Limit: -1}, code: "INVALID_LIMIT"},
		{name: "limit above 50", in: SearchInput{ProjectID: "ao", Text: "flaky", Limit: 51}, code: "INVALID_LIMIT"},
		{name: "project required", in: SearchInput{Text: "flaky"}, code: "PROJECT_REQUIRED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(&fakeRunner{}, &fakeSessions{sessions: map[domain.SessionID]domain.Session{}}, nil)

			_, err := svc.Search(context.Background(), tt.in)

			kind, code := apiCode(t, err)
			if kind != apierr.KindInvalid || code != tt.code {
				t.Fatalf("error kind=%v code=%q, want invalid %q", kind, code, tt.code)
			}
		})
	}
}

func TestSearch(t *testing.T) {
	rows := []Row{{"section": "lesson", "id": "l1", "kind": "fix", "text": "t"}}
	tests := []struct {
		name        string
		in          SearchInput
		wantProject string
		wantExclude string
	}{
		{name: "explicit project", in: SearchInput{ProjectID: "ao", Text: "flaky sqlite", Limit: 7}, wantProject: "ao"},
		{name: "session derives project and exclude", in: SearchInput{SessionID: "s1", ProjectID: "ignored", Text: "flaky sqlite", Limit: 7}, wantProject: "proj", wantExclude: "host1/s1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeRunner{rows: rows}
			svc := newTestService(r, &fakeSessions{sessions: map[domain.SessionID]domain.Session{"s1": worker("s1", "proj")}}, nil)

			items, err := svc.Search(context.Background(), tt.in)

			if err != nil {
				t.Fatalf("Search() error = %v", err)
			}
			if len(items) != 1 || items[0].ID != "l1" {
				t.Fatalf("items = %#v", items)
			}
			p := r.snapshot()[0].params
			if p["project"] != tt.wantProject || p["exclude"] != tt.wantExclude || p["limit"] != 7 {
				t.Fatalf("params = %#v", p)
			}
		})
	}
}

func TestRateLessonValidationAndStoredTarget(t *testing.T) {
	validSessions := &fakeSessions{sessions: map[domain.SessionID]domain.Session{"s1": worker("s1", "ao")}}
	tests := []struct {
		name string
		in   RatingInput
		code string
	}{
		{"missing session", RatingInput{LessonID: "l1", Rating: "wrong"}, "SESSION_ID_REQUIRED"},
		{"missing lesson", RatingInput{SessionID: "s1", Rating: "wrong"}, "LESSON_ID_REQUIRED"},
		{"invalid rating", RatingInput{SessionID: "s1", LessonID: "l1", Rating: "bad"}, "INVALID_RATING"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeRunner{}
			_, code := apiCode(t, newTestService(r, validSessions, nil).RateLesson(context.Background(), tt.in))
			if code != tt.code || len(r.snapshot()) != 0 {
				t.Fatalf("code=%s calls=%d", code, len(r.snapshot()))
			}
		})
	}
	r := &fakeRunner{rows: []Row{{"id": "stored"}}}
	err := newTestService(r, validSessions, nil).RateLesson(context.Background(), RatingInput{SessionID: "s1", LessonID: "stored", Rating: "useful"})
	if err != nil {
		t.Fatal(err)
	}
	call := r.snapshot()[0]
	if call.params["id"] != "stored" || call.params["taskKey"] != "host1/s1" {
		t.Fatalf("rating params = %#v", call.params)
	}
}

func TestRetireLessonValidation(t *testing.T) {
	for _, tt := range []struct{ id, reason, code string }{{"", "obsolete", "LESSON_ID_REQUIRED"}, {"l1", " ", "RETIRE_REASON_REQUIRED"}} {
		r := &fakeRunner{}
		_, code := apiCode(t, newTestService(r, &fakeSessions{}, nil).RetireLesson(context.Background(), tt.id, tt.reason))
		if code != tt.code || len(r.snapshot()) != 0 {
			t.Fatalf("code=%s calls=%d", code, len(r.snapshot()))
		}
	}
}

func TestSearchWithoutTermsReturnsEmpty(t *testing.T) {
	r := &fakeRunner{}
	svc := newTestService(r, &fakeSessions{sessions: map[domain.SessionID]domain.Session{}}, nil)

	items, err := svc.Search(context.Background(), SearchInput{ProjectID: "ao", Text: "the a ok"})

	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("items = %#v, want empty non-nil", items)
	}
	if len(r.snapshot()) != 0 {
		t.Fatalf("graph must not be queried without terms")
	}
}

func TestSearchErrors(t *testing.T) {
	tests := []struct {
		name     string
		sessions *fakeSessions
		runErr   error
		in       SearchInput
		wantKind apierr.Kind
		wantCode string
	}{
		{name: "unknown session", sessions: &fakeSessions{sessions: map[domain.SessionID]domain.Session{}}, in: SearchInput{SessionID: "nope", Text: "flaky"}, wantKind: apierr.KindNotFound, wantCode: "SESSION_NOT_FOUND"},
		{name: "graph down", sessions: &fakeSessions{sessions: map[domain.SessionID]domain.Session{}}, runErr: errors.New("down"), in: SearchInput{ProjectID: "ao", Text: "flaky"}, wantKind: apierr.KindUnavailable, wantCode: "KNOWLEDGE_GRAPH_UNAVAILABLE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(&fakeRunner{err: tt.runErr}, tt.sessions, nil)

			_, err := svc.Search(context.Background(), tt.in)

			kind, code := apiCode(t, err)
			if kind != tt.wantKind || code != tt.wantCode {
				t.Fatalf("error kind=%v code=%q, want %v %q", kind, code, tt.wantKind, tt.wantCode)
			}
		})
	}
}

func TestPriorContextReturnsLessonIDsOnly(t *testing.T) {
	r := &fakeRunner{rows: []Row{
		{"section": "attempt", "id": "host1/s0", "title": "old"},
		{"section": "lesson", "id": "l1", "text": "one"},
		{"section": "lesson", "id": "", "text": "no id"},
		{"section": "task", "id": "host1/s9", "title": "similar"},
		{"section": "lesson", "id": "l2", "text": "two"},
	}}
	svc := newTestService(r, &fakeSessions{}, nil)

	section, ids, err := svc.PriorContext(context.Background(), "ao", "7", "flaky sqlite")

	if err != nil {
		t.Fatalf("PriorContext() error = %v", err)
	}
	if !reflect.DeepEqual(ids, []string{"l1", "l2"}) {
		t.Fatalf("ids = %#v", ids)
	}
	if !strings.HasPrefix(section, "## Knowledge Graph") || !strings.Contains(section, "- [tip] (l1) one") {
		t.Fatalf("section = %q", section)
	}
}

func TestPriorContextReturnsGraphError(t *testing.T) {
	svc := newTestService(&fakeRunner{err: errors.New("down")}, &fakeSessions{}, nil)

	section, ids, err := svc.PriorContext(context.Background(), "ao", "7", "")

	if err == nil || section != "" || ids != nil {
		t.Fatalf("PriorContext() = %q, %v, %v; want error only", section, ids, err)
	}
}

func TestServiceMarkServedUsesTaskKey(t *testing.T) {
	r := &fakeRunner{}
	svc := newTestService(r, &fakeSessions{}, nil)

	if err := svc.MarkServed(context.Background(), "s1", []string{"l1"}); err != nil {
		t.Fatalf("MarkServed() error = %v", err)
	}

	p := r.snapshot()[0].params
	if p["taskKey"] != "host1/s1" || p["at"] != "2026-10-23T11:00:00Z" {
		t.Fatalf("params = %#v", p)
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	svc := New(Deps{})

	if svc.log == nil || svc.now == nil || svc.newID == nil {
		t.Fatalf("New() left defaults unset")
	}
	if a, b := svc.newID(), svc.newID(); a == b {
		t.Fatalf("default NewID must be unique")
	}
	if cap(svc.queue) != queueSize {
		t.Fatalf("queue cap = %d, want %d", cap(svc.queue), queueSize)
	}
}

// fakeEmbedder returns vec for every input and records what it embedded.
type fakeEmbedder struct {
	mu      sync.Mutex
	vec     []float64
	err     error
	docErr  error
	queries []string
	docs    [][]string
}

func (f *fakeEmbedder) EmbedQuery(_ context.Context, text string) ([]float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, text)
	if f.err != nil {
		return nil, f.err
	}
	return f.vec, nil
}

func (f *fakeEmbedder) EmbedDocuments(_ context.Context, texts []string) ([][]float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.docs = append(f.docs, append([]string(nil), texts...))
	if f.docErr != nil {
		return nil, f.docErr
	}
	out := make([][]float64, len(texts))
	for i := range texts {
		out[i] = f.vec
	}
	return out, nil
}

func (f *fakeEmbedder) queryCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.queries...)
}

func (f *fakeEmbedder) docCalls() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]string(nil), f.docs...)
}

func newSemanticService(r Runner, e Embedder, logs *syncBuffer) *Service {
	return New(Deps{
		Graph: NewGraph(r), Embedder: e, MinScore: 0.8, HostID: "host1",
		Sessions: &fakeSessions{sessions: map[domain.SessionID]domain.Session{"s1": worker("s1", "ao")}},
		Logger:   slog.New(slog.NewTextHandler(logs, nil)),
		Now:      func() time.Time { return testAt }, NewID: func() string { return "lesson-1" },
	})
}

func TestSearchWithEmbedderUsesVector(t *testing.T) {
	r := &fakeRunner{}
	e := &fakeEmbedder{vec: []float64{0.3, 0.4}}
	svc := newSemanticService(r, e, &syncBuffer{})

	if _, err := svc.Search(context.Background(), SearchInput{ProjectID: "ao", Text: "flaky sqlite"}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if got := e.queryCalls(); !reflect.DeepEqual(got, []string{"flaky sqlite"}) {
		t.Fatalf("queries = %v", got)
	}
	call := r.snapshot()[0]
	if !reflect.DeepEqual(call.params["vector"], []float64{0.3, 0.4}) || call.params["minScore"] != 0.8 {
		t.Fatalf("params vector=%v minScore=%v", call.params["vector"], call.params["minScore"])
	}
	if !strings.Contains(call.statement, lessonsVectorCypher) {
		t.Fatalf("statement lacks vector search")
	}
}

func TestPriorContextFallsBackToFullTextOnEmbedError(t *testing.T) {
	r := &fakeRunner{}
	logs := &syncBuffer{}
	svc := newSemanticService(r, &fakeEmbedder{err: errors.New("ollama down")}, logs)

	if _, _, err := svc.PriorContext(context.Background(), "ao", "", "flaky sqlite"); err != nil {
		t.Fatalf("PriorContext() error = %v", err)
	}

	call := r.snapshot()[0]
	if !strings.Contains(call.statement, lessonsCypher) || strings.Contains(call.statement, lessonsVectorCypher) {
		t.Fatalf("want full-text search after embed failure:\n%s", call.statement)
	}
	if v := call.params["vector"].([]float64); len(v) != 0 || call.params["minScore"] != 0.0 {
		t.Fatalf("params vector=%v minScore=%v, want unset", v, call.params["minScore"])
	}
	if got := logs.String(); !strings.Contains(got, "query embedding failed") || !strings.Contains(got, "ollama down") {
		t.Fatalf("logs = %q", got)
	}
}

func TestPriorContextSkipsEmbedWithoutTerms(t *testing.T) {
	r := &fakeRunner{}
	e := &fakeEmbedder{vec: []float64{1}}
	svc := newSemanticService(r, e, &syncBuffer{})

	if _, _, err := svc.PriorContext(context.Background(), "ao", "7", "the a ok"); err != nil {
		t.Fatalf("PriorContext() error = %v", err)
	}

	if n := len(e.queryCalls()); n != 0 {
		t.Fatalf("embed calls = %d, want 0", n)
	}
	if v := r.snapshot()[0].params["vector"].([]float64); len(v) != 0 {
		t.Fatalf("vector = %v, want empty", v)
	}
}

func TestAddLessonKicksEmbedLoop(t *testing.T) {
	svc := newSemanticService(&fakeRunner{}, &fakeEmbedder{}, &syncBuffer{})

	if _, err := svc.AddLesson(context.Background(), LessonInput{SessionID: "s1", Text: "x"}); err != nil {
		t.Fatalf("AddLesson() error = %v", err)
	}

	if len(svc.embedKick) != 1 {
		t.Fatalf("embedKick len = %d, want 1", len(svc.embedKick))
	}
}

func TestAddLessonFailureDoesNotKick(t *testing.T) {
	svc := newSemanticService(&fakeRunner{err: errors.New("down")}, &fakeEmbedder{}, &syncBuffer{})

	_, _ = svc.AddLesson(context.Background(), LessonInput{SessionID: "s1", Text: "x"})

	if len(svc.embedKick) != 0 {
		t.Fatalf("embedKick len = %d, want 0", len(svc.embedKick))
	}
}

func TestKickEmbedNeverBlocks(t *testing.T) {
	svc := New(Deps{})
	done := make(chan struct{})

	go func() {
		svc.kickEmbed()
		svc.kickEmbed()
		svc.kickEmbed()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("kickEmbed blocked on a pending kick")
	}
	if cap(svc.embedKick) != 1 || len(svc.embedKick) != 1 {
		t.Fatalf("embedKick len=%d cap=%d, want 1/1", len(svc.embedKick), cap(svc.embedKick))
	}
}

const (
	storedLesson   = "make ci-check fails while another session runs golangci-lint; wait and rerun."
	incomingLesson = "If make ci-check reports a parallel golangci-lint run, wait for it and rerun."
)

// dedupRunner answers the lookup with matches and the confirm with
// confirmRows; every other statement succeeds empty.
func dedupRunner(matches []Row, lookupErr error, confirmRows []Row) *fakeRunner {
	return dedupRunnerWithConfirmErr(matches, lookupErr, confirmRows, nil)
}

func dedupRunnerWithConfirmErr(matches []Row, lookupErr error, confirmRows []Row, confirmErr error) *fakeRunner {
	return &fakeRunner{reply: func(statement string) ([]Row, error) {
		switch {
		case strings.Contains(statement, "RETURN l.id AS id, l.text AS text, score"):
			return matches, lookupErr
		case statement == confirmLessonCypher:
			return confirmRows, confirmErr
		}
		return nil, nil
	}}
}

func newDedupService(r Runner, e Embedder, logs *syncBuffer) *Service {
	svc := newSemanticService(r, e, logs)
	svc.mergeScore = 0.94
	return svc
}

func statements(r *fakeRunner) []string {
	var out []string
	for _, c := range r.snapshot() {
		switch {
		case strings.Contains(c.statement, "RETURN l.id AS id, l.text AS text, score"):
			out = append(out, "lookup")
		case c.statement == confirmLessonCypher:
			out = append(out, "confirm")
		case c.statement == addLessonCypher:
			out = append(out, "add")
		case strings.Contains(c.statement, "setNodeVectorProperty"):
			out = append(out, "embed")
		default:
			out = append(out, "other")
		}
	}
	return out
}

func TestAddLessonMergesIntoSameClaim(t *testing.T) {
	r := dedupRunner([]Row{{"id": "old-1", "text": storedLesson, "score": 0.95}}, nil, []Row{{"id": "old-1"}})
	e := &fakeEmbedder{vec: []float64{0.6, 0.8}}
	svc := newDedupService(r, e, &syncBuffer{})

	added, err := svc.AddLesson(context.Background(), LessonInput{SessionID: "s1", Kind: "pitfall", Text: incomingLesson, Topics: []string{"lint"}})

	if err != nil {
		t.Fatalf("AddLesson() error = %v", err)
	}
	if added != (AddedLesson{ID: "old-1", Merged: true}) {
		t.Fatalf("added = %+v, want merged into old-1", added)
	}
	if got := statements(r); !reflect.DeepEqual(got, []string{"lookup", "confirm"}) {
		t.Fatalf("statements = %v, want lookup then confirm only", got)
	}
	lookup, confirm := r.snapshot()[0].params, r.snapshot()[1].params
	if lookup["project"] != "ao" || lookup["kind"] != "pitfall" || lookup["minScore"] != 0.94 {
		t.Fatalf("lookup params = %#v", lookup)
	}
	if confirm["id"] != "old-1" || confirm["text"] != incomingLesson || confirm["taskKey"] != "host1/s1" ||
		!reflect.DeepEqual(confirm["topics"], []string{"lint"}) {
		t.Fatalf("confirm params = %#v, want the new wording kept on the edge", confirm)
	}
	if got := e.docCalls(); !reflect.DeepEqual(got, [][]string{{incomingLesson}}) {
		t.Fatalf("embedded = %v, want the lesson as a document", got)
	}
}

func TestAddLessonSkipsContradictingMatch(t *testing.T) {
	r := dedupRunner([]Row{
		{"id": "contra", "text": "Never rebase onto dev before opening the PR.", "score": 0.97},
		{"id": "same", "text": "Rebase onto dev first, then open the PR.", "score": 0.95},
	}, nil, []Row{{"id": "same"}})
	svc := newDedupService(r, &fakeEmbedder{vec: []float64{1}}, &syncBuffer{})

	added, err := svc.AddLesson(context.Background(), LessonInput{SessionID: "s1", Text: "Always rebase onto dev before opening the PR."})

	if err != nil || added != (AddedLesson{ID: "same", Merged: true}) {
		t.Fatalf("AddLesson() = %+v, %v, want merged into the non-contradicting match", added, err)
	}
	if id := r.snapshot()[1].params["id"]; id != "same" {
		t.Fatalf("confirmed %v, want same", id)
	}
}

func TestAddLessonStoresNewWhenNoMatchMakesTheSameClaim(t *testing.T) {
	r := dedupRunner([]Row{{"id": "old", "text": "Set minScore to 0.70.", "score": 0.99}}, nil, nil)
	e := &fakeEmbedder{vec: []float64{0.6, 0.8}}
	svc := newDedupService(r, e, &syncBuffer{})

	added, err := svc.AddLesson(context.Background(), LessonInput{SessionID: "s1", Text: "Set minScore to 0.90."})

	if err != nil || added != (AddedLesson{ID: "lesson-1"}) {
		t.Fatalf("AddLesson() = %+v, %v, want a new lesson", added, err)
	}
	if got := statements(r); !reflect.DeepEqual(got, []string{"lookup", "add", "embed"}) {
		t.Fatalf("statements = %v, want lookup, add, embed", got)
	}
	embed := r.snapshot()[2].params["lessons"].([]map[string]any)
	if len(embed) != 1 || embed[0]["id"] != "lesson-1" || !reflect.DeepEqual(embed[0]["v"], []float64{0.6, 0.8}) {
		t.Fatalf("stored vectors = %#v", embed)
	}
	if len(svc.embedKick) != 0 {
		t.Fatalf("embed loop kicked although the vector was stored")
	}
}

func TestAddLessonCreatesWhenMatchVanished(t *testing.T) {
	r := dedupRunner([]Row{{"id": "gone", "text": storedLesson, "score": 0.96}}, nil, nil)
	svc := newDedupService(r, &fakeEmbedder{vec: []float64{1}}, &syncBuffer{})

	added, err := svc.AddLesson(context.Background(), LessonInput{SessionID: "s1", Text: incomingLesson})

	if err != nil || added != (AddedLesson{ID: "lesson-1"}) {
		t.Fatalf("AddLesson() = %+v, %v, want a new lesson", added, err)
	}
	if got := statements(r); !reflect.DeepEqual(got, []string{"lookup", "confirm", "add", "embed"}) {
		t.Fatalf("statements = %v", got)
	}
}

func TestAddLessonDegradesToPlainCreate(t *testing.T) {
	tests := []struct {
		name       string
		embedder   *fakeEmbedder
		lookupErr  error
		mergeScore float64
		wantStmts  []string
		wantLog    string
		wantKick   int
	}{
		{name: "embedding fails", embedder: &fakeEmbedder{docErr: errors.New("ollama down")}, mergeScore: 0.94, wantStmts: []string{"add"}, wantLog: "lesson embedding failed", wantKick: 1},
		{name: "lookup fails", embedder: &fakeEmbedder{vec: []float64{1}}, lookupErr: errors.New("dims mismatch"), mergeScore: 0.94, wantStmts: []string{"lookup", "add", "embed"}, wantLog: "duplicate lesson lookup failed"},
		{name: "merging disabled", embedder: &fakeEmbedder{vec: []float64{1}}, mergeScore: 0, wantStmts: []string{"add", "embed"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := dedupRunner([]Row{{"id": "old", "text": storedLesson, "score": 0.99}}, tt.lookupErr, []Row{{"id": "old"}})
			logs := &syncBuffer{}
			svc := newSemanticService(r, tt.embedder, logs)
			svc.mergeScore = tt.mergeScore

			added, err := svc.AddLesson(context.Background(), LessonInput{SessionID: "s1", Text: incomingLesson})

			if err != nil || added != (AddedLesson{ID: "lesson-1"}) {
				t.Fatalf("AddLesson() = %+v, %v, want the lesson stored", added, err)
			}
			if got := statements(r); !reflect.DeepEqual(got, tt.wantStmts) {
				t.Fatalf("statements = %v, want %v", got, tt.wantStmts)
			}
			if !strings.Contains(logs.String(), tt.wantLog) {
				t.Fatalf("logs = %q, want %q", logs.String(), tt.wantLog)
			}
			if len(svc.embedKick) != tt.wantKick {
				t.Fatalf("embedKick len = %d, want %d", len(svc.embedKick), tt.wantKick)
			}
		})
	}
}

func TestAddLessonKicksEmbedLoopWhenStoringVectorFails(t *testing.T) {
	r := &fakeRunner{reply: func(statement string) ([]Row, error) {
		if strings.Contains(statement, "setNodeVectorProperty") {
			return nil, errors.New("dims mismatch")
		}
		return nil, nil
	}}
	logs := &syncBuffer{}
	svc := newDedupService(r, &fakeEmbedder{vec: []float64{1}}, logs)

	if _, err := svc.AddLesson(context.Background(), LessonInput{SessionID: "s1", Text: "x"}); err != nil {
		t.Fatalf("AddLesson() error = %v", err)
	}

	if len(svc.embedKick) != 1 || !strings.Contains(logs.String(), "store lesson embedding failed") {
		t.Fatalf("embedKick len = %d logs = %q", len(svc.embedKick), logs.String())
	}
}

func TestAddLessonCreatesWhenConfirmFails(t *testing.T) {
	r := dedupRunnerWithConfirmErr([]Row{
		{"id": "old", "text": storedLesson, "score": 0.97},
		{"id": "other", "text": storedLesson, "score": 0.95},
	}, nil, []Row{{"id": "old"}}, errors.New("timeout"))
	logs := &syncBuffer{}
	svc := newDedupService(r, &fakeEmbedder{vec: []float64{1}}, logs)

	added, err := svc.AddLesson(context.Background(), LessonInput{SessionID: "s1", Text: incomingLesson})

	if err != nil || added != (AddedLesson{ID: "lesson-1"}) {
		t.Fatalf("AddLesson() = %+v, %v, want a new lesson", added, err)
	}
	if got := statements(r); !reflect.DeepEqual(got, []string{"lookup", "confirm", "add", "embed"}) {
		t.Fatalf("statements = %v, want one confirm attempt then create", got)
	}
	if !strings.Contains(logs.String(), "confirm lesson failed") {
		t.Fatalf("logs = %q", logs.String())
	}
}
