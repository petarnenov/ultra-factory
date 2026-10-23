package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

type knowledgeCapture struct {
	method string
	path   string
	query  url.Values
	lesson knowledgeLessonRequest
}

// knowledgeServer answers every request with status and body and records
// the last request.
func knowledgeServer(t *testing.T, status int, body string) *knowledgeCapture {
	t.Helper()
	cfg := setConfigEnv(t)
	got := &knowledgeCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.query = r.Method, r.URL.Path, r.URL.Query()
		if r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&got.lesson)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)
	return got
}

func TestKnowledgeValidation(t *testing.T) {
	sixTopics := []string{"knowledge", "add", "x"}
	for _, topic := range []string{"a", "b", "c", "d", "e", "f"} {
		sixTopics = append(sixTopics, "--topic", topic)
	}
	tests := []struct {
		name    string
		session string
		args    []string
	}{
		{name: "bad kind", session: "worker-1", args: []string{"knowledge", "add", "--kind", "rumor", "x"}},
		{name: "empty text", session: "worker-1", args: []string{"knowledge", "add", "  "}},
		{name: "text too long", session: "worker-1", args: []string{"knowledge", "add", strings.Repeat("界", 1001)}},
		{name: "too many topics", session: "worker-1", args: sixTopics},
		{name: "missing session for add", args: []string{"knowledge", "add", "x"}},
		{name: "empty search", session: "worker-1", args: []string{"knowledge", "search"}},
		{name: "limit zero", session: "worker-1", args: []string{"knowledge", "search", "--limit", "0", "x"}},
		{name: "limit above 50", session: "worker-1", args: []string{"knowledge", "search", "--limit", "51", "x"}},
		{name: "search without project or session", args: []string{"knowledge", "search", "flaky"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AO_SESSION_ID", tc.session)

			_, _, err := executeCLI(t, Deps{}, tc.args...)

			if err == nil || ExitCode(err) != 2 {
				t.Fatalf("err=%v exit=%d", err, ExitCode(err))
			}
		})
	}
}

func TestKnowledgeAdd(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want knowledgeLessonRequest
	}{
		{
			name: "default kind",
			args: []string{"knowledge", "add", "run", "make", "sqlc"},
			want: knowledgeLessonRequest{SessionID: "worker-1", Kind: "tip", Text: "run make sqlc"},
		},
		{
			name: "kind and topics",
			args: []string{"knowledge", "add", "--kind", "pitfall", "--topic", "sqlite", "--topic", "ci", "  WAL locks  "},
			want: knowledgeLessonRequest{SessionID: "worker-1", Kind: "pitfall", Text: "WAL locks", Topics: []string{"sqlite", "ci"}},
		},
		{
			name: "text at the limit",
			args: []string{"knowledge", "add", strings.Repeat("界", 1000)},
			want: knowledgeLessonRequest{SessionID: "worker-1", Kind: "tip", Text: strings.Repeat("界", 1000)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AO_SESSION_ID", " worker-1 ")
			got := knowledgeServer(t, http.StatusCreated, `{"id":"lesson-9"}`)

			out, _, err := executeCLI(t, aliveDeps(), tc.args...)

			if err != nil {
				t.Fatal(err)
			}
			if got.method != http.MethodPost || got.path != "/api/v1/knowledge/lessons" {
				t.Fatalf("request = %s %s", got.method, got.path)
			}
			if !reflect.DeepEqual(got.lesson, tc.want) {
				t.Fatalf("body = %+v, want %+v", got.lesson, tc.want)
			}
			if out != "Recorded lesson lesson-9\n" {
				t.Fatalf("out = %q", out)
			}
		})
	}
}

func TestKnowledgeSearchQuery(t *testing.T) {
	tests := []struct {
		name    string
		session string
		args    []string
		want    url.Values
	}{
		{
			name: "explicit project wins over session", session: "worker-1",
			args: []string{"knowledge", "search", "--project", "ao", "--limit", "9", "flaky", "sqlite"},
			want: url.Values{"q": {"flaky sqlite"}, "limit": {"9"}, "projectId": {"ao"}},
		},
		{
			name: "session from environment", session: "worker-1",
			args: []string{"knowledge", "search", "flaky"},
			want: url.Values{"q": {"flaky"}, "limit": {"5"}, "sessionId": {"worker-1"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AO_SESSION_ID", tc.session)
			got := knowledgeServer(t, http.StatusOK, `{"items":[]}`)

			out, _, err := executeCLI(t, aliveDeps(), tc.args...)

			if err != nil {
				t.Fatal(err)
			}
			if got.method != http.MethodGet || got.path != "/api/v1/knowledge/search" {
				t.Fatalf("request = %s %s", got.method, got.path)
			}
			if !reflect.DeepEqual(got.query, tc.want) {
				t.Fatalf("query = %v, want %v", got.query, tc.want)
			}
			if out != "No matching knowledge.\n" {
				t.Fatalf("out = %q", out)
			}
		})
	}
}

const knowledgeSearchBody = `{"items":[
{"section":"lesson","id":"l1","kind":"fix","title":"src","outcome":"merged","text":"run make sqlc","lessons":[],"score":2.5},
{"section":"attempt","id":"h/s0","kind":"","title":"Old try","outcome":"closed","text":"done: gave up","lessons":["use -race"],"score":1},
{"section":"task","id":"h/s9","kind":"","title":"Similar","outcome":"merged","text":"","lessons":[],"score":0.5}]}`

func TestKnowledgeSearchTextOutput(t *testing.T) {
	t.Setenv("AO_SESSION_ID", "worker-1")
	knowledgeServer(t, http.StatusOK, knowledgeSearchBody)

	out, _, err := executeCLI(t, aliveDeps(), "knowledge", "search", "flaky")

	if err != nil {
		t.Fatal(err)
	}
	want := "lesson   [fix] run make sqlc\n" +
		"attempt  [closed] Old try — done: gave up\n" +
		"         lesson: use -race\n" +
		"task     [merged] Similar\n"
	if out != want {
		t.Fatalf("out =\n%s\nwant\n%s", out, want)
	}
}

func TestKnowledgeSearchJSONOutput(t *testing.T) {
	t.Setenv("AO_SESSION_ID", "worker-1")
	knowledgeServer(t, http.StatusOK, knowledgeSearchBody)

	out, _, err := executeCLI(t, aliveDeps(), "knowledge", "search", "--json", "flaky")

	if err != nil {
		t.Fatal(err)
	}
	var got knowledgeSearchResponse
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if len(got.Items) != 3 || got.Items[1].Lessons[0] != "use -race" || got.Items[0].Score != 2.5 {
		t.Fatalf("items = %+v", got.Items)
	}
}

func TestKnowledgeDaemonErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "add", args: []string{"knowledge", "add", "x"}},
		{name: "search", args: []string{"knowledge", "search", "flaky"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AO_SESSION_ID", "worker-1")
			knowledgeServer(t, http.StatusServiceUnavailable,
				`{"error":"unavailable","message":"graph down","code":"KNOWLEDGE_GRAPH_UNAVAILABLE","requestId":"req-7"}`)

			_, _, err := executeCLI(t, aliveDeps(), tc.args...)

			if err == nil || ExitCode(err) != 1 || !strings.Contains(err.Error(), "graph down (KNOWLEDGE_GRAPH_UNAVAILABLE) [request req-7]") {
				t.Fatalf("err=%v exit=%d", err, ExitCode(err))
			}
		})
	}
}
