package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type sessionInputCapture struct {
	mu       sync.Mutex
	requests []recordedSessionInputRequest
}

type recordedSessionInputRequest struct {
	method string
	path   string
	body   string
}

func (c *sessionInputCapture) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, recordedSessionInputRequest{
		method: r.Method,
		path:   r.URL.Path,
		body:   string(body),
	})
}

func (c *sessionInputCapture) all() []recordedSessionInputRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]recordedSessionInputRequest(nil), c.requests...)
}

const sessionInputSnapshotJSON = `{
  "sessionId": "demo-1",
  "activities": [
    {
      "kind": "activity",
      "activityKind": "command",
      "status": "completed",
      "summary": "list files",
      "requestId": "",
      "detail": {"command": "ls"}
    },
    {
      "kind": "activity",
      "activityKind": "user_input",
      "status": "pending",
      "summary": "Should I run the review?",
      "requestId": "acp-request:host:1",
      "detail": {
        "inputMode": "form",
        "message": "Should I run the review?",
        "schema": {
          "type": "object",
          "properties": {
            "q0": {
              "type": "string",
              "title": "Run review?",
              "oneOf": [{"const": "Yes", "title": "Yes"}, {"const": "No", "title": "No"}]
            }
          },
          "required": ["q0"]
        }
      }
    },
    {
      "kind": "activity",
      "activityKind": "user_input",
      "status": "pending",
      "summary": "Open the docs?",
      "requestId": "acp-request:host:2",
      "detail": {
        "inputMode": "url",
        "message": "Open the docs?",
        "url": "https://example.com/docs"
      }
    },
    {
      "kind": "activity",
      "activityKind": "user_input",
      "status": "resolved",
      "summary": "An older question",
      "requestId": "acp-request:host:0",
      "detail": {"inputMode": "form", "message": "An older question"}
    }
  ]
}`

func sessionInputServer(t *testing.T) (*httptest.Server, *sessionInputCapture) {
	t.Helper()
	capture := &sessionInputCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/internal/") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		capture.record(r)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sessions/demo-1/conversation":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, sessionInputSnapshotJSON)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sessions/empty/conversation":
			_, _ = io.WriteString(w, `{"sessionId":"empty","activities":[]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sessions/missing/conversation":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"not_found","code":"SESSION_NOT_FOUND","message":"session not found"}`)
		case r.Method == http.MethodPost &&
			r.URL.Path == "/api/v1/sessions/demo-1/conversation/inputs/acp-request:host:1/resolve":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost &&
			r.URL.Path == "/api/v1/sessions/demo-1/conversation/inputs/stale/resolve":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":"conflict","code":"CHAT_REQUEST_NOT_PENDING","message":"this request has already been answered or is no longer waiting"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, capture
}

func TestSessionInputListsPendingInputs(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := sessionInputServer(t)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, aliveDeps(), "session", "input", "demo-1")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	for _, want := range []string{
		"session demo-1 has 2 pending structured inputs",
		"acp-request:host:1",
		"Should I run the review?",
		"q0 (Run review?) [string, required] values: \"Yes\", \"No\"",
		"https://example.com/docs",
		"records consent; it does not open the URL",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "acp-request:host:0") || strings.Contains(out, "An older question") {
		t.Fatalf("output included a resolved input:\n%s", out)
	}
	for _, request := range capture.all() {
		if request.method != http.MethodGet {
			t.Fatalf("list sent %s %s; listing must not resolve anything", request.method, request.path)
		}
	}
}

func TestSessionInputJSONListsPendingInputs(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := sessionInputServer(t)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, aliveDeps(), "session", "input", "demo-1", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	var list sessionInputList
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if list.SessionID != "demo-1" || len(list.Inputs) != 2 {
		t.Fatalf("list = %+v, want two pending inputs", list)
	}
	if list.Inputs[0].RequestID != "acp-request:host:1" ||
		list.Inputs[0].Mode != "form" ||
		list.Inputs[0].Schema == nil {
		t.Fatalf("form input = %+v", list.Inputs[0])
	}
	if list.Inputs[1].URL != "https://example.com/docs" {
		t.Fatalf("url input = %+v", list.Inputs[1])
	}
}

func TestSessionInputReportsNoPendingInputs(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := sessionInputServer(t)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, aliveDeps(), "session", "input", "empty")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if !strings.Contains(out, "session empty has no pending structured inputs") {
		t.Fatalf("output = %q", out)
	}
}

func TestSessionInputResolvesFormWithTypedAnswers(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := sessionInputServer(t)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, aliveDeps(),
		"session", "input", "demo-1",
		"--request-id", "acp-request:host:1",
		"--action", "accept",
		"--answer", "q0=No",
		"--answer", "count=2",
		"--answer", "flag=true")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if !strings.Contains(out, "Resolved pending input acp-request:host:1 on session demo-1 (accept).") {
		t.Fatalf("output = %q", out)
	}
	requests := capture.all()
	if len(requests) != 1 || requests[0].method != http.MethodPost ||
		requests[0].path != "/api/v1/sessions/demo-1/conversation/inputs/acp-request:host:1/resolve" {
		t.Fatalf("requests = %+v", requests)
	}
	var body struct {
		Action  string         `json:"action"`
		Content map[string]any `json:"content"`
	}
	if err := json.Unmarshal([]byte(requests[0].body), &body); err != nil {
		t.Fatalf("decode body: %v\n%s", err, requests[0].body)
	}
	if body.Action != "accept" || body.Content["q0"] != "No" ||
		body.Content["count"] != float64(2) || body.Content["flag"] != true {
		t.Fatalf("body = %+v", body)
	}
}

func TestSessionInputResolvesDeclineWithoutContent(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := sessionInputServer(t)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, aliveDeps(),
		"session", "input", "demo-1",
		"--request-id", "acp-request:host:1",
		"--action", "decline",
		"--json")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	var resolved sessionInputResolved
	if err := json.Unmarshal([]byte(out), &resolved); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if resolved.SessionID != "demo-1" || resolved.RequestID != "acp-request:host:1" ||
		resolved.Action != "decline" || resolved.Status != "resolved" {
		t.Fatalf("resolved = %+v", resolved)
	}
	requests := capture.all()
	if len(requests) != 1 {
		t.Fatalf("requests = %+v", requests)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(requests[0].body), &body); err != nil {
		t.Fatal(err)
	}
	if _, present := body["content"]; present {
		t.Fatalf("decline body included content: %s", requests[0].body)
	}
}

func TestSessionInputUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "request id without action", args: []string{"session", "input", "demo-1", "--request-id", "x"}},
		{name: "action without request id", args: []string{"session", "input", "demo-1", "--action", "accept"}},
		{name: "invalid action", args: []string{"session", "input", "demo-1", "--request-id", "x", "--action", "answer"}},
		{name: "answer with decline", args: []string{"session", "input", "demo-1", "--request-id", "x", "--action", "decline", "--answer", "q0=No"}},
		{name: "malformed answer", args: []string{"session", "input", "demo-1", "--request-id", "x", "--action", "accept", "--answer", "no-equals"}},
		{name: "empty answer key", args: []string{"session", "input", "demo-1", "--request-id", "x", "--action", "accept", "--answer", "=value"}},
		{name: "repeated answer", args: []string{"session", "input", "demo-1", "--request-id", "x", "--action", "accept", "--answer", "q0=a", "--answer", "q0=b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := setConfigEnv(t)
			srv, capture := sessionInputServer(t)
			writeRunFileFor(t, cfg, srv)

			_, _, err := executeCLI(t, aliveDeps(), tc.args...)
			if err == nil || ExitCode(err) != 2 {
				t.Fatalf("err = %v, want usage error (exit 2)", err)
			}
			if requests := capture.all(); len(requests) != 0 {
				t.Fatalf("usage error reached the daemon: %+v", requests)
			}
		})
	}
}

func TestSessionInputUnknownSessionIsRuntimeFailure(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := sessionInputServer(t)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, aliveDeps(), "session", "input", "missing")
	if err == nil || ExitCode(err) != 1 {
		t.Fatalf("err = %v, want runtime failure (exit 1)", err)
	}
	if !strings.Contains(err.Error(), "SESSION_NOT_FOUND") {
		t.Fatalf("err = %v, want the daemon error envelope", err)
	}
}

func TestSessionInputAlreadyResolvedRequestIsRuntimeFailure(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := sessionInputServer(t)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, aliveDeps(),
		"session", "input", "demo-1", "--request-id", "stale", "--action", "accept")
	if err == nil || ExitCode(err) != 1 {
		t.Fatalf("err = %v, want runtime failure (exit 1)", err)
	}
	if !strings.Contains(err.Error(), "CHAT_REQUEST_NOT_PENDING") {
		t.Fatalf("err = %v, want the daemon error envelope", err)
	}
}

func TestSessionInputRemoteHostListsAndResolves(t *testing.T) {
	cfg := setConfigEnv(t)
	startLocalDaemon(t, cfg, "host-local", nil)
	remote := newFakeRemoteHost(t, "host-build", "pw")
	remote.handle(http.MethodGet, "/api/v1/sessions/widgets-3/conversation",
		func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(w, http.StatusOK, map[string]any{
				"sessionId": "widgets-3",
				"activities": []any{map[string]any{
					"activityKind": "user_input",
					"status":       "pending",
					"summary":      "Deploy now?",
					"requestId":    "acp-request:build:4",
					"detail": map[string]any{
						"inputMode": "form",
						"message":   "Deploy now?",
						"schema": map[string]any{
							"type":       "object",
							"properties": map[string]any{"q0": map[string]any{"type": "string"}},
							"required":   []any{"q0"},
						},
					},
				}},
			})
		})
	remote.handle(http.MethodPost,
		"/api/v1/sessions/widgets-3/conversation/inputs/acp-request:build:4/resolve",
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	writeRemotesFile(t, remoteEntry{
		Label: "build-box", URL: remote.srv.URL, Password: "pw", HostID: "host-build",
	})

	out, errOut, err := executeCLI(t, aliveDeps(),
		"session", "input", "widgets-3", "--host", "build-box")
	if err != nil {
		t.Fatalf("unexpected list error: %v\nstderr=%s", err, errOut)
	}
	if !strings.Contains(out, "acp-request:build:4") || !strings.Contains(out, "Deploy now?") {
		t.Fatalf("output = %q", out)
	}
	if !strings.Contains(out, "ao session input widgets-3 --host build-box") {
		t.Fatalf("answer hint does not keep the remote host: %q", out)
	}

	_, errOut, err = executeCLI(t, aliveDeps(),
		"session", "input", "widgets-3", "--host", "build-box",
		"--request-id", "acp-request:build:4",
		"--action", "accept",
		"--answer", "q0=Yes")
	if err != nil {
		t.Fatalf("unexpected resolve error: %v\nstderr=%s", err, errOut)
	}

	resolve, ok := remote.find(http.MethodPost,
		"/api/v1/sessions/widgets-3/conversation/inputs/acp-request:build:4/resolve")
	if !ok {
		t.Fatalf("resolve never reached the remote host; saw %+v", remote.recorded())
	}
	if resolve.auth != "Bearer pw" || resolve.expectedHost != "host-build" {
		t.Fatalf("resolve headers auth=%q expected=%q", resolve.auth, resolve.expectedHost)
	}
	var body resolveSessionInputRequest
	if err := json.Unmarshal([]byte(resolve.body), &body); err != nil {
		t.Fatalf("decode remote body: %v\n%s", err, resolve.body)
	}
	if body.Action != "accept" || body.Content["q0"] != "Yes" {
		t.Fatalf("remote body = %+v", body)
	}
}
