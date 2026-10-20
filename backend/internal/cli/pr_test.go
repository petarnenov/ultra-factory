package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	mergeTestURL  = "https://github.com/acme/repo/pull/42"
	mergeTestHead = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// mergeDaemon fakes the daemon routes merge uses and applies the real
// route's body validation.
type mergeDaemon struct {
	sessions    string // body of GET /sessions
	summaries   map[string]string
	mergeBody   string
	mergeStatus int
	mergeReply  string // overrides the 200 body
	// mergeErrBody overrides the body of a mergeStatus refusal.
	mergeErrBody string
	requests     []string
}

func (d *mergeDaemon) start(t *testing.T, cfg testConfig) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.requests = append(d.requests, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sessions":
			_, _ = io.WriteString(w, d.sessions)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pr"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/"), "/pr")
			_, _ = io.WriteString(w, d.summaries[id])
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/prs/42/merge":
			raw, _ := io.ReadAll(r.Body)
			d.mergeBody = string(raw)
			var in struct {
				PRURL           string `json:"prUrl"`
				ExpectedHeadSHA string `json:"expectedHeadSha"`
			}
			if err := json.Unmarshal(raw, &in); err != nil || in.PRURL == "" || len(in.ExpectedHeadSHA) < 40 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":{"code":"INVALID_PR","message":"PR URL and expected head SHA are required"}}`)
				return
			}
			if d.mergeStatus != 0 {
				w.WriteHeader(d.mergeStatus)
				refusal := d.mergeErrBody
				if refusal == "" {
					refusal = `{"error":"conflict","code":"PR_HEAD_CHANGED","message":"PR head changed; refresh before merging"}`
				}
				_, _ = io.WriteString(w, refusal)
				return
			}
			reply := d.mergeReply
			if reply == "" {
				reply = `{"ok":true,"prNumber":42,"method":"squash"}`
			}
			_, _ = io.WriteString(w, reply)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)
}

func ownedMergeDaemon() *mergeDaemon {
	return &mergeDaemon{
		sessions: `{"sessions":[
			{"id":"demo-1","projectId":"p","kind":"worker","prs":[{"url":"` + mergeTestURL + `","number":42}]},
			{"id":"root-1","projectId":"p","kind":"orchestrator","prs":[]}]}`,
		summaries: map[string]string{
			"demo-1": `{"prs":[{"url":"` + mergeTestURL + `","number":42,"headSha":"` + strings.ToUpper(mergeTestHead) + `"}]}`,
		},
	}
}

func TestPRMergePostsURLAndHeadForSessionOwnedPR(t *testing.T) {
	cfg := setReviewEnv(t)
	d := ownedMergeDaemon()
	d.start(t, cfg)

	out, errOut, err := executeCLI(t, aliveDeps(), "pr", "merge", "#42")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	var body mergePRRequest
	if err := json.Unmarshal([]byte(d.mergeBody), &body); err != nil {
		t.Fatalf("merge body %q: %v", d.mergeBody, err)
	}
	if body.PRURL != mergeTestURL || body.ExpectedHeadSHA != mergeTestHead {
		t.Fatalf("merge body = %+v, want url %s head %s", body, mergeTestURL, mergeTestHead)
	}
	if !strings.Contains(out, "merged PR #42 using squash") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestPRMergeSendsRequestingSession(t *testing.T) {
	cfg := setReviewEnv(t)
	t.Setenv("AO_SESSION_ID", " ao-root ")
	d := ownedMergeDaemon()
	d.start(t, cfg)

	if _, errOut, err := executeCLI(t, aliveDeps(), "pr", "merge", "42"); err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	var body mergePRRequest
	if err := json.Unmarshal([]byte(d.mergeBody), &body); err != nil {
		t.Fatalf("body %q: %v", d.mergeBody, err)
	}
	if body.RequestedBySessionID != "ao-root" || body.PRURL != mergeTestURL || body.ExpectedHeadSHA != mergeTestHead {
		t.Fatalf("body = %+v, want requester ao-root with url and head", body)
	}
}

func TestPRMergeSurfacesMergeRootOnlyEnvelope(t *testing.T) {
	cfg := setReviewEnv(t)
	t.Setenv("AO_SESSION_ID", "ao-3")
	d := ownedMergeDaemon()
	d.mergeStatus = http.StatusForbidden
	d.mergeErrBody = `{"error":"forbidden","code":"MERGE_ROOT_ONLY","message":"Only the project root may merge a pull request","requestId":"req-7"}`
	d.start(t, cfg)

	_, _, err := executeCLI(t, aliveDeps(), "pr", "merge", "42")
	if err == nil || ExitCode(err) != 1 {
		t.Fatalf("err = %v exit = %d, want exit 1", err, ExitCode(err))
	}
	for _, want := range []string{"MERGE_ROOT_ONLY", "request req-7"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestPRMergeHeadFlagPinsReviewedCommit(t *testing.T) {
	cfg := setReviewEnv(t)
	d := ownedMergeDaemon()
	d.start(t, cfg)
	pinned := strings.Repeat("b", 40)

	if _, errOut, err := executeCLI(t, aliveDeps(), "pr", "merge", "42", "--head", pinned); err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if !strings.Contains(d.mergeBody, `"expectedHeadSha":"`+pinned+`"`) {
		t.Fatalf("merge body = %s, want pinned head %s", d.mergeBody, pinned)
	}
	for _, req := range d.requests {
		if strings.HasSuffix(req, "/pr") {
			t.Fatalf("pinned head must not consult the tracked head, got %q", req)
		}
	}
}

func TestPRMergeRejectsShortHeadFlag(t *testing.T) {
	setReviewEnv(t)
	_, _, err := executeCLI(t, aliveDeps(), "pr", "merge", "42", "--head", "abc123")
	if got := ExitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2; err=%v", got, err)
	}
}

func TestPRMergeFailsWhenNoSessionTracksPR(t *testing.T) {
	cfg := setReviewEnv(t)
	d := &mergeDaemon{sessions: `{"sessions":[{"id":"demo-1","projectId":"p","kind":"worker","prs":[]}]}`}
	d.start(t, cfg)

	_, _, err := executeCLI(t, aliveDeps(), "pr", "merge", "42")
	if err == nil || !strings.Contains(err.Error(), "no AO session tracks PR #42") {
		t.Fatalf("err = %v", err)
	}
	if d.mergeBody != "" {
		t.Fatalf("merge must not be posted, body = %s", d.mergeBody)
	}
}

func TestPRMergeAmbiguousNumberAsksForProject(t *testing.T) {
	cfg := setReviewEnv(t)
	other := "https://github.com/acme/other/pull/42"
	d := &mergeDaemon{sessions: `{"sessions":[
		{"id":"a-1","projectId":"a","kind":"worker","prs":[{"url":"` + mergeTestURL + `","number":42}]},
		{"id":"b-1","projectId":"b","kind":"worker","prs":[{"url":"` + other + `","number":42}]}]}`}
	d.start(t, cfg)

	_, _, err := executeCLI(t, aliveDeps(), "pr", "merge", "42")
	if got := ExitCode(err); got != 2 || !strings.Contains(err.Error(), "--project") {
		t.Fatalf("exit=%d err=%v", got, err)
	}
}

func TestPRMergeFailsWithoutKnownHead(t *testing.T) {
	cfg := setReviewEnv(t)
	d := ownedMergeDaemon()
	d.summaries["demo-1"] = `{"prs":[{"url":"` + mergeTestURL + `","number":42,"headSha":""}]}`
	d.start(t, cfg)

	_, _, err := executeCLI(t, aliveDeps(), "pr", "merge", "42")
	if err == nil || !strings.Contains(err.Error(), "no known head commit") {
		t.Fatalf("err = %v", err)
	}
	if d.mergeBody != "" {
		t.Fatalf("merge must not be posted, body = %s", d.mergeBody)
	}
}

func TestPRMergeSurfacesHeadChangedRefusal(t *testing.T) {
	cfg := setReviewEnv(t)
	d := ownedMergeDaemon()
	d.mergeStatus = http.StatusConflict
	d.start(t, cfg)

	_, _, err := executeCLI(t, aliveDeps(), "pr", "merge", "42")
	if err == nil || !strings.Contains(err.Error(), "PR_HEAD_CHANGED") {
		t.Fatalf("err = %v", err)
	}
}

func TestPRMergeOmitsMissingMethodFromOutput(t *testing.T) {
	cfg := setReviewEnv(t)
	d := ownedMergeDaemon()
	d.mergeReply = `{"ok":true,"prNumber":42}`
	d.start(t, cfg)
	out, errOut, err := executeCLI(t, aliveDeps(), "pr", "merge", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if out != "merged PR #42\n" {
		t.Fatalf("stdout = %q, want %q", out, "merged PR #42\n")
	}
}

func TestPRMergeRejectsInvalidNumber(t *testing.T) {
	setReviewEnv(t)

	for _, number := range []string{"0", "-1", "abc", "#"} {
		t.Run(number, func(t *testing.T) {
			_, _, err := executeCLI(t, aliveDeps(), "pr", "merge", number)
			if got := ExitCode(err); got != 2 {
				t.Fatalf("exit code = %d, want 2; err=%v", got, err)
			}
		})
	}
}

func TestPRMergeRequiresExactlyOneArgument(t *testing.T) {
	setReviewEnv(t)

	for _, args := range [][]string{
		{"pr", "merge"},
		{"pr", "merge", "42", "43"},
	} {
		_, _, err := executeCLI(t, aliveDeps(), args...)
		if got := ExitCode(err); got != 2 {
			t.Fatalf("args = %v, exit code = %d, want 2; err=%v", args, got, err)
		}
	}
}

func TestPRMergeSurfacesDaemonError(t *testing.T) {
	cfg := setReviewEnv(t)
	srv, _ := reviewServer(t, http.StatusConflict, `{"message":"PR is not mergeable","code":"PR_NOT_MERGEABLE","requestId":"req-1"}`)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, aliveDeps(), "pr", "merge", "42")
	if got := ExitCode(err); got != 1 {
		t.Fatalf("exit code = %d, want 1; err=%v", got, err)
	}
	for _, want := range []string{"PR is not mergeable", "PR_NOT_MERGEABLE", "req-1"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %q, want %q", err, want)
		}
	}
}

func TestPRResolveCommentsPostsIDs(t *testing.T) {
	cfg := setReviewEnv(t)
	srv, capture := reviewServer(t, http.StatusOK, `{"ok":true,"resolved":2}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, aliveDeps(), "pr", "resolve-comments", "42", "thread-1", "thread-2")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodPost || capture.path != "/api/v1/prs/42/resolve-comments" {
		t.Fatalf("request = %s %s", capture.method, capture.path)
	}
	var req resolveCommentsRequest
	if err := json.Unmarshal([]byte(capture.body), &req); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(req.CommentIDs) != 2 || req.CommentIDs[0] != "thread-1" || req.CommentIDs[1] != "thread-2" {
		t.Fatalf("comment ids = %v", req.CommentIDs)
	}
	if !strings.Contains(out, "resolved 2 review thread(s) on PR #42") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestPRResolveCommentsAllowsNoIDs(t *testing.T) {
	cfg := setReviewEnv(t)
	srv, capture := reviewServer(t, http.StatusOK, `{"ok":true,"resolved":3}`)
	writeRunFileFor(t, cfg, srv)

	if _, errOut, err := executeCLI(t, aliveDeps(), "pr", "resolve-comments", "42"); err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if strings.TrimSpace(capture.body) != "{}" {
		t.Fatalf("body = %q, want {}", capture.body)
	}
}

func TestPRResolveCommentsRequiresPRNumber(t *testing.T) {
	setReviewEnv(t)

	_, _, err := executeCLI(t, aliveDeps(), "pr", "resolve-comments")
	if got := ExitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2; err=%v", got, err)
	}
}
