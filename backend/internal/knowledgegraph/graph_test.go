package knowledgegraph

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type runCall struct {
	statement string
	params    map[string]any
}

// fakeRunner records every statement and replies with rows or err.
type fakeRunner struct {
	mu    sync.Mutex
	calls []runCall
	rows  []Row
	err   error
	ran   chan runCall
}

func (f *fakeRunner) Run(_ context.Context, statement string, params map[string]any) ([]Row, error) {
	call := runCall{statement: statement, params: params}
	f.mu.Lock()
	f.calls = append(f.calls, call)
	rows, err := f.rows, f.err
	f.mu.Unlock()
	if f.ran != nil {
		f.ran <- call
	}
	return rows, err
}

func (f *fakeRunner) snapshot() []runCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]runCall(nil), f.calls...)
}

var testAt = time.Date(2026, 10, 23, 12, 0, 0, 0, time.FixedZone("x", 3600))

func TestRecordTask(t *testing.T) {
	tests := []struct {
		name      string
		fact      TaskFact
		wantProps map[string]any
		wantIssue string
	}{
		{
			name:      "empty fields omitted",
			fact:      TaskFact{Key: "h/s1", SessionID: "s1", Host: "h", Project: "ao", At: testAt},
			wantProps: map[string]any{},
		},
		{
			name: "all fields set",
			fact: TaskFact{
				Key: "h/s1", SessionID: "s1", Host: "h", Project: "ao", Title: "Fix", Issue: "#42",
				Outcome: "merged", Harness: "claude-code", Model: "m", Effort: "high",
				PRs: []string{"https://x/pr/1"}, ElapsedSeconds: 60, InputTokens: 10, OutputTokens: 5,
				Stages: "building 60s x1", Report: "done: ok", At: testAt,
			},
			wantProps: map[string]any{
				"title": "Fix", "issue": "#42", "outcome": "merged", "harness": "claude-code",
				"model": "m", "effort": "high", "stages": "building 60s x1",
				"prs": []string{"https://x/pr/1"}, "elapsedSeconds": int64(60),
				"inputTokens": int64(10), "outputTokens": int64(5),
			},
			wantIssue: "ao/42",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeRunner{}

			err := NewGraph(r).RecordTask(context.Background(), tt.fact)

			if err != nil {
				t.Fatalf("RecordTask() error = %v", err)
			}
			calls := r.snapshot()
			if len(calls) != 1 || calls[0].statement != recordTaskCypher {
				t.Fatalf("calls = %+v", calls)
			}
			p := calls[0].params
			if !reflect.DeepEqual(p["props"], tt.wantProps) {
				t.Fatalf("props = %#v, want %#v", p["props"], tt.wantProps)
			}
			if p["issueKey"] != tt.wantIssue || p["report"] != tt.fact.Report || p["key"] != "h/s1" {
				t.Fatalf("params = %#v", p)
			}
			if p["at"] != "2026-10-23T11:00:00Z" {
				t.Fatalf("at = %v, want UTC RFC3339", p["at"])
			}
		})
	}
}

func TestRecordTaskReturnsRunnerError(t *testing.T) {
	r := &fakeRunner{err: errors.New("down")}

	err := NewGraph(r).RecordTask(context.Background(), TaskFact{At: testAt})

	if err == nil || err.Error() != "down" {
		t.Fatalf("RecordTask() error = %v", err)
	}
}

func TestAddLesson(t *testing.T) {
	tests := []struct {
		name   string
		topics []string
		want   []string
	}{
		{name: "nil topics become empty list", topics: nil, want: []string{}},
		{name: "topics pass through", topics: []string{"git"}, want: []string{"git"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeRunner{}
			l := Lesson{ID: "l1", TaskKey: "h/s1", Session: "s1", Host: "h", Project: "ao", Kind: "fix", Text: "t", Topics: tt.topics, At: testAt}

			err := NewGraph(r).AddLesson(context.Background(), l)

			if err != nil {
				t.Fatalf("AddLesson() error = %v", err)
			}
			p := r.snapshot()[0].params
			if !reflect.DeepEqual(p["topics"], tt.want) {
				t.Fatalf("topics = %#v, want %#v", p["topics"], tt.want)
			}
			if p["id"] != "l1" || p["taskKey"] != "h/s1" || p["kind"] != "fix" || p["project"] != "ao" {
				t.Fatalf("params = %#v", p)
			}
		})
	}
}

func TestMarkServed(t *testing.T) {
	tests := []struct {
		name      string
		ids       []string
		wantCalls int
	}{
		{name: "no ids is a no-op", ids: nil, wantCalls: 0},
		{name: "ids link lessons", ids: []string{"l1", "l2"}, wantCalls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeRunner{}

			err := NewGraph(r).MarkServed(context.Background(), "h/s1", tt.ids, testAt)

			if err != nil {
				t.Fatalf("MarkServed() error = %v", err)
			}
			calls := r.snapshot()
			if len(calls) != tt.wantCalls {
				t.Fatalf("calls = %d, want %d", len(calls), tt.wantCalls)
			}
			if tt.wantCalls == 1 && !reflect.DeepEqual(calls[0].params["ids"], tt.ids) {
				t.Fatalf("ids = %#v", calls[0].params["ids"])
			}
		})
	}
}

func TestPriorBuildsSubqueries(t *testing.T) {
	tests := []struct {
		name      string
		query     Query
		wantCalls int
		wantParts int
		wantLimit int
		hasAttach bool
		hasText   bool
	}{
		{name: "no issue and no terms skips the graph", query: Query{Project: "ao", Text: "a an"}},
		{name: "issue only", query: Query{Project: "ao", Issue: "#7"}, wantCalls: 1, wantParts: 1, wantLimit: 5, hasAttach: true},
		{name: "text only", query: Query{Project: "ao", Text: "flaky sqlite test", Limit: 3}, wantCalls: 1, wantParts: 2, wantLimit: 3, hasText: true},
		{name: "issue and text", query: Query{Project: "ao", Issue: "7", Text: "flaky sqlite"}, wantCalls: 1, wantParts: 3, wantLimit: 5, hasAttach: true, hasText: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeRunner{}

			items, err := NewGraph(r).Prior(context.Background(), tt.query)

			if err != nil {
				t.Fatalf("Prior() error = %v", err)
			}
			calls := r.snapshot()
			if len(calls) != tt.wantCalls {
				t.Fatalf("calls = %d, want %d", len(calls), tt.wantCalls)
			}
			if tt.wantCalls == 0 {
				if items != nil {
					t.Fatalf("items = %#v, want nil", items)
				}
				return
			}
			st := calls[0].statement
			if got := strings.Count(st, "CALL () {"); got != tt.wantParts {
				t.Fatalf("subqueries = %d, want %d\n%s", got, tt.wantParts, st)
			}
			if got := strings.Count(st, "UNION ALL"); got != tt.wantParts-1 {
				t.Fatalf("UNION ALL = %d, want %d", got, tt.wantParts-1)
			}
			if strings.Contains(st, attemptsCypher) != tt.hasAttach {
				t.Fatalf("attempts part present = %v", !tt.hasAttach)
			}
			if strings.Contains(st, lessonsCypher) != tt.hasText || strings.Contains(st, tasksCypher) != tt.hasText {
				t.Fatalf("text parts present mismatch")
			}
			p := calls[0].params
			if p["limit"] != tt.wantLimit || p["project"] != "ao" {
				t.Fatalf("params = %#v", p)
			}
			if tt.hasAttach && p["issueKey"] != "ao/7" {
				t.Fatalf("issueKey = %v", p["issueKey"])
			}
		})
	}
}

func TestPriorMapsRows(t *testing.T) {
	r := &fakeRunner{rows: []Row{{
		"section": "attempt", "id": "h/s0", "kind": "", "title": "Old try", "outcome": "closed",
		"text": "done: gave up", "lessons": []any{"use -race"}, "score": float64(1),
	}}}

	items, err := NewGraph(r).Prior(context.Background(), Query{Project: "ao", Issue: "7", ExcludeTask: "h/s1"})

	if err != nil {
		t.Fatalf("Prior() error = %v", err)
	}
	want := []Item{{Section: "attempt", ID: "h/s0", Title: "Old try", Outcome: "closed", Text: "done: gave up", Lessons: []string{"use -race"}, Score: 1}}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("items = %#v, want %#v", items, want)
	}
	if r.snapshot()[0].params["exclude"] != "h/s1" {
		t.Fatalf("exclude not passed")
	}
}

func TestPriorReturnsRunnerError(t *testing.T) {
	r := &fakeRunner{err: errors.New("down")}

	_, err := NewGraph(r).Prior(context.Background(), Query{Project: "ao", Issue: "7"})

	if err == nil {
		t.Fatalf("Prior() error = nil, want runner error")
	}
}

func TestIssueKey(t *testing.T) {
	tests := []struct{ project, issue, want string }{
		{"ao", "#12", "ao/12"},
		{"ao", " 12 ", "ao/12"},
		{"", "12", ""},
		{"ao", "#", ""},
	}
	for _, tt := range tests {
		t.Run(tt.project+"|"+tt.issue, func(t *testing.T) {
			if got := issueKey(tt.project, tt.issue); got != tt.want {
				t.Fatalf("issueKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSearchTerms(t *testing.T) {
	var many []string
	for i := 0; i < 30; i++ {
		many = append(many, fmt.Sprintf("word%02d", i))
	}
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "empty", text: "", want: ""},
		{name: "dedupe and lowercase", text: "Flaky flaky FLAKY test", want: "flaky OR test"},
		{name: "stop words dropped", text: "the issue with github and the work", want: ""},
		{name: "short words dropped", text: "go is ok db sql", want: "sql"},
		{name: "lucene syntax stripped", text: `foo:bar AND (x) "quoted"~2 title:*`, want: "foo OR bar OR quoted OR title"},
		{name: "unicode letters kept", text: "грешка в тестa", want: "грешка OR тестa"},
		{name: "at most 24 terms", text: strings.Join(many, " "), want: strings.Join(many[:24], " OR ")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SearchTerms(tt.text); got != tt.want {
				t.Fatalf("SearchTerms(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestRecordTaskClearsEmbeddingOnNewReport(t *testing.T) {
	_, afterReport, found := strings.Cut(recordTaskCypher, "WHEN $report = ''")
	reportClause, _, _ := strings.Cut(afterReport, "MERGE (p:Project")

	if !found || !strings.Contains(reportClause, "t.embedding = null") {
		t.Fatalf("report clause must null t.embedding:\n%s", recordTaskCypher)
	}
}

func TestPriorWithVectorUsesVectorParts(t *testing.T) {
	tests := []struct {
		name      string
		query     Query
		wantParts int
		wantK     int
	}{
		{name: "vector with text", query: Query{Project: "ao", Text: "flaky sqlite", Limit: 3}, wantParts: 2, wantK: 60},
		{name: "vector without terms", query: Query{Project: "ao", Text: "the a", Limit: 2}, wantParts: 2, wantK: 40},
		{name: "vector with issue", query: Query{Project: "ao", Issue: "7", Text: "flaky"}, wantParts: 3, wantK: 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeRunner{}
			vec := []float64{0.1, 0.2}
			q := tt.query
			q.Vector, q.MinScore = vec, 0.7

			if _, err := NewGraph(r).Prior(context.Background(), q); err != nil {
				t.Fatalf("Prior() error = %v", err)
			}

			call := r.snapshot()[0]
			st := call.statement
			if got := strings.Count(st, "CALL () {"); got != tt.wantParts {
				t.Fatalf("subqueries = %d, want %d", got, tt.wantParts)
			}
			if !strings.Contains(st, lessonsVectorCypher) || !strings.Contains(st, tasksVectorCypher) {
				t.Fatalf("vector parts missing:\n%s", st)
			}
			if strings.Contains(st, lessonsCypher) || strings.Contains(st, tasksCypher) {
				t.Fatalf("full-text parts must not run with a vector")
			}
			p := call.params
			if !reflect.DeepEqual(p["vector"], vec) || p["minScore"] != 0.7 || p["k"] != tt.wantK {
				t.Fatalf("params vector=%v minScore=%v k=%v", p["vector"], p["minScore"], p["k"])
			}
		})
	}
}

func TestPriorWithoutVectorSendsEmptyVector(t *testing.T) {
	r := &fakeRunner{}

	if _, err := NewGraph(r).Prior(context.Background(), Query{Project: "ao", Text: "flaky sqlite"}); err != nil {
		t.Fatalf("Prior() error = %v", err)
	}

	got, ok := r.snapshot()[0].params["vector"].([]float64)
	if !ok || got == nil || len(got) != 0 {
		t.Fatalf("vector = %#v, want empty non-nil slice", r.snapshot()[0].params["vector"])
	}
}

func similarityRows() []Row {
	row := func(section, id string, sim float64) Row {
		return Row{"section": section, "id": id, "score": sim, "similarity": sim}
	}
	return []Row{
		row("attempt", "a1", 0.1),
		row("lesson", "l1", 0.90),
		row("lesson", "l2", 0.87),
		row("lesson", "l3", 0.80),
		row("task", "t1", 0.75),
		row("task", "t2", 0.60),
	}
}

func itemIDs(items []Item) []string {
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	return ids
}

func TestPriorKeepsNearTopMatchesWithVector(t *testing.T) {
	r := &fakeRunner{rows: similarityRows()}

	items, err := NewGraph(r).Prior(context.Background(), Query{Project: "ao", Issue: "7", Vector: []float64{1}})

	if err != nil {
		t.Fatalf("Prior() error = %v", err)
	}
	if got := itemIDs(items); !reflect.DeepEqual(got, []string{"a1", "l1", "l2", "t1"}) {
		t.Fatalf("ids = %v, want attempt plus near-top per section", got)
	}
	if items[1].similarity != 0.90 {
		t.Fatalf("similarity = %v, want mapped from row", items[1].similarity)
	}
}

func TestPriorKeepsAllMatchesWithoutVector(t *testing.T) {
	r := &fakeRunner{rows: similarityRows()}

	items, err := NewGraph(r).Prior(context.Background(), Query{Project: "ao", Issue: "7", Text: "flaky"})

	if err != nil {
		t.Fatalf("Prior() error = %v", err)
	}
	if got := itemIDs(items); len(got) != 6 {
		t.Fatalf("ids = %v, want all rows", got)
	}
}

func TestNearTop(t *testing.T) {
	item := func(section, id string, sim float64) Item {
		return Item{Section: section, ID: id, similarity: sim}
	}
	tests := []struct {
		name  string
		items []Item
		want  []string
	}{
		{name: "empty", items: nil, want: []string{}},
		{name: "attempt always kept", items: []Item{item("attempt", "a", 0), item("attempt", "b", 1)}, want: []string{"a", "b"}},
		{
			name:  "margin per section",
			items: []Item{item("lesson", "l1", 0.5), item("task", "t1", 0.95), item("lesson", "l2", 0.47), item("task", "t2", 0.9)},
			want:  []string{"l1", "t1", "l2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := itemIDs(nearTop(tt.items)); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("nearTop() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMissingEmbeddings(t *testing.T) {
	r := &fakeRunner{rows: []Row{
		{"label": "Lesson", "id": "l1", "text": "run make sqlc"},
		{"label": "Task", "id": "h/s1", "text": "Fix\ndone: ok"},
	}}

	got, err := NewGraph(r).MissingEmbeddings(context.Background(), 7, 1024)

	if err != nil {
		t.Fatalf("MissingEmbeddings() error = %v", err)
	}
	want := []EmbedTarget{{Label: "Lesson", ID: "l1", Text: "run make sqlc"}, {Label: "Task", ID: "h/s1", Text: "Fix\ndone: ok"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targets = %#v, want %#v", got, want)
	}
	call := r.snapshot()[0]
	if call.params["limit"] != 7 || call.params["dims"] != 1024 {
		t.Fatalf("params = %#v", call.params)
	}
	// A vector from an earlier model must count as missing.
	if !strings.Contains(call.statement, "size(l.embedding) <> $dims") ||
		!strings.Contains(call.statement, "size(t.embedding) <> $dims") {
		t.Fatalf("statement does not re-embed wrong-sized vectors:\n%s", call.statement)
	}
}

func TestMissingEmbeddingsReturnsRunnerError(t *testing.T) {
	r := &fakeRunner{err: errors.New("down")}

	got, err := NewGraph(r).MissingEmbeddings(context.Background(), 1, 1024)

	if err == nil || got != nil {
		t.Fatalf("MissingEmbeddings() = %v, %v; want error", got, err)
	}
}

func TestSetEmbeddings(t *testing.T) {
	lesson := EmbedTarget{Label: "Lesson", ID: "l1"}
	task := EmbedTarget{Label: "Task", ID: "h/s1"}
	tests := []struct {
		name        string
		targets     []EmbedTarget
		vecs        [][]float64
		wantLessons []map[string]any
		wantTasks   []map[string]any
	}{
		{
			name: "split by label", targets: []EmbedTarget{task, lesson}, vecs: [][]float64{{1}, {2}},
			wantLessons: []map[string]any{{"id": "l1", "v": []float64{2}}},
			wantTasks:   []map[string]any{{"id": "h/s1", "v": []float64{1}}},
		},
		{
			name: "no tasks sends empty list", targets: []EmbedTarget{lesson}, vecs: [][]float64{{2}},
			wantLessons: []map[string]any{{"id": "l1", "v": []float64{2}}},
			wantTasks:   []map[string]any{},
		},
		{
			name: "no lessons sends empty list", targets: []EmbedTarget{task}, vecs: [][]float64{{1}},
			wantLessons: []map[string]any{},
			wantTasks:   []map[string]any{{"id": "h/s1", "v": []float64{1}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeRunner{}

			if err := NewGraph(r).SetEmbeddings(context.Background(), tt.targets, tt.vecs); err != nil {
				t.Fatalf("SetEmbeddings() error = %v", err)
			}

			p := r.snapshot()[0].params
			if !reflect.DeepEqual(p["lessons"], tt.wantLessons) || !reflect.DeepEqual(p["tasks"], tt.wantTasks) {
				t.Fatalf("lessons = %#v, tasks = %#v", p["lessons"], p["tasks"])
			}
		})
	}
}

func TestSetEmbeddingsReturnsRunnerError(t *testing.T) {
	r := &fakeRunner{err: errors.New("down")}

	err := NewGraph(r).SetEmbeddings(context.Background(), []EmbedTarget{{Label: "Lesson", ID: "l1"}}, [][]float64{{1}})

	if err == nil || err.Error() != "down" {
		t.Fatalf("SetEmbeddings() error = %v", err)
	}
}
