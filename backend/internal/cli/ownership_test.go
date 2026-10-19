package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type ownerCapture struct {
	method string
	path   string
	query  string
	body   string
}

func ownershipServer(t *testing.T, status int, response string) (*httptest.Server, *ownerCapture) {
	t.Helper()
	capture := &ownerCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/internal/telemetry/cli-invoked" {
			_, _ = io.WriteString(w, `{}`)
			return
		}
		capture.method = r.Method
		capture.path = r.URL.Path
		capture.query = r.URL.RawQuery
		capture.body = string(body)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(srv.Close)
	return srv, capture
}

func runOwnershipCLI(t *testing.T, srv *httptest.Server, args ...string) (string, string, error) {
	t.Helper()
	cfg := setConfigEnv(t)
	writeRunFileFor(t, cfg, srv)
	return executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, args...)
}

func TestSessionAssignDefaultsTheCallerToAOSessionID(t *testing.T) {
	srv, capture := ownershipServer(t, http.StatusOK, `{"session":{"id":"demo-1","projectId":"demo","kind":"worker","ownerSessionId":"demo-2"}}`)
	t.Setenv("AO_SESSION_ID", "demo-root")

	out, errOut, err := runOwnershipCLI(t, srv, "session", "assign", "demo-1", "--owner", "demo-2")
	if err != nil {
		t.Fatalf("session assign failed: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodPut || capture.path != "/api/v1/sessions/demo-1/owner" {
		t.Fatalf("request = %s %s", capture.method, capture.path)
	}
	var body sessionOwnerRequest
	if err := json.Unmarshal([]byte(capture.body), &body); err != nil {
		t.Fatalf("body %q: %v", capture.body, err)
	}
	if body.OwnerSessionID == nil || *body.OwnerSessionID != "demo-2" || body.RequestedBySessionID != "demo-root" {
		t.Fatalf("body = %+v", body)
	}
	if !strings.Contains(out, "session demo-1 owner: demo-2") {
		t.Fatalf("output = %s", out)
	}
}

func TestSessionAssignRootSendsANullOwner(t *testing.T) {
	srv, capture := ownershipServer(t, http.StatusOK, `{"session":{"id":"demo-1","projectId":"demo","kind":"worker","ownerSessionId":null}}`)
	t.Setenv("AO_SESSION_ID", "demo-root")

	out, errOut, err := runOwnershipCLI(t, srv, "session", "assign", "demo-1", "--owner", "root")
	if err != nil {
		t.Fatalf("session assign failed: %v\nstderr=%s", err, errOut)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(capture.body), &body); err != nil {
		t.Fatalf("body %q: %v", capture.body, err)
	}
	if value, ok := body["ownerSessionId"]; !ok || value != nil {
		t.Fatalf("ownerSessionId = %#v, want explicit null", value)
	}
	if !strings.Contains(out, "session demo-1 owner: root") {
		t.Fatalf("output = %s", out)
	}
}

func TestSessionAssignUsageErrorsExitTwo(t *testing.T) {
	t.Setenv("AO_SESSION_ID", "")
	cfg := setConfigEnv(t)
	_ = cfg

	cases := [][]string{
		{"session", "assign", "demo-1"},
		{"session", "assign", "demo-1", "--owner", "demo-2"},
		{"session", "assign", "demo-1", "--owner", "demo-2", "--host", "build-box"},
		{"session", "assign"},
	}
	for _, args := range cases {
		_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, args...)
		if err == nil || ExitCode(err) != 2 {
			t.Fatalf("%v: err=%v exit=%d, want usage error 2", args, err, ExitCode(err))
		}
	}
}

func TestSessionAssignDaemonConflictExitsOneWithTheEnvelope(t *testing.T) {
	srv, _ := ownershipServer(t, http.StatusConflict,
		`{"error":"conflict","code":"OWNER_REASSIGNMENT_IN_FLIGHT","message":"A report of this worker is claimed for delivery","requestId":"req-9"}`)
	t.Setenv("AO_SESSION_ID", "demo-root")

	_, _, err := runOwnershipCLI(t, srv, "session", "assign", "demo-1", "--owner", "demo-2")
	if err == nil {
		t.Fatal("expected a daemon conflict error")
	}
	if ExitCode(err) != 1 {
		t.Fatalf("exit = %d, want 1", ExitCode(err))
	}
	for _, want := range []string{"OWNER_REASSIGNMENT_IN_FLIGHT", "request req-9"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestSessionOwnerShowsCoordinatorAndRoot(t *testing.T) {
	srv, capture := ownershipServer(t, http.StatusOK,
		`{"sessionId":"demo-1","projectId":"demo","kind":"worker","ownerSessionId":"demo-2","ownerKind":"coordinator","ownerScope":"auth","rootSessionId":"demo-root"}`)

	out, errOut, err := runOwnershipCLI(t, srv, "session", "owner", "demo-1")
	if err != nil {
		t.Fatalf("session owner failed: %v\nstderr=%s", err, errOut)
	}
	if capture.path != "/api/v1/sessions/demo-1/ownership" {
		t.Fatalf("request path = %s", capture.path)
	}
	for _, want := range []string{"owner: demo-2 (coordinator, scope: auth)", "root: demo-root", "role: worker"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q missing %q", out, want)
		}
	}
}

func TestSessionOwnerJSONRoundTrips(t *testing.T) {
	srv, _ := ownershipServer(t, http.StatusOK,
		`{"sessionId":"demo-3","projectId":"demo","kind":"coordinator","ownerSessionId":null,"coordinatorScope":"auth","rootSessionId":"demo-root"}`)

	out, errOut, err := runOwnershipCLI(t, srv, "session", "owner", "demo-3", "--json")
	if err != nil {
		t.Fatalf("session owner --json failed: %v\nstderr=%s", err, errOut)
	}
	var got sessionOwnershipDTO
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not decodable: %v\n%s", err, out)
	}
	if got.Kind != "coordinator" || got.CoordinatorScope != "auth" || got.OwnerSessionID != nil {
		t.Fatalf("ownership = %+v", got)
	}
}

func TestSessionOwnerNeverCallsACoordinatorRootOwned(t *testing.T) {
	srv, _ := ownershipServer(t, http.StatusOK,
		`{"sessionId":"demo-3","projectId":"demo","kind":"coordinator","ownerSessionId":null,"coordinatorScope":"auth","rootSessionId":"demo-root"}`)

	out, errOut, err := runOwnershipCLI(t, srv, "session", "owner", "demo-3")
	if err != nil {
		t.Fatalf("session owner failed: %v\nstderr=%s", err, errOut)
	}
	if strings.Contains(out, "owner:") {
		t.Fatalf("a coordinator is never owned; output = %q", out)
	}
	for _, want := range []string{"role: coordinator", "coordinator scope: auth", "root: demo-root"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q missing %q", out, want)
		}
	}
}

func TestCoordinatorListShowsTheHierarchy(t *testing.T) {
	srv, capture := ownershipServer(t, http.StatusOK,
		`{"projectId":"demo","rootSessionId":"demo-root","coordinators":[{"coordinatorSessionId":"demo-c1","coordinatorScope":"auth","isTerminated":false,"workerSessionIds":["demo-1","demo-2"],"workerCount":2}],"rootWorkerSessionIds":["demo-9"]}`)

	out, errOut, err := runOwnershipCLI(t, srv, "coordinator", "ls", "--project", "demo")
	if err != nil {
		t.Fatalf("coordinator ls failed: %v\nstderr=%s", err, errOut)
	}
	if capture.path != "/api/v1/projects/demo/coordinators" {
		t.Fatalf("request path = %s", capture.path)
	}
	for _, want := range []string{"root: demo-root", "coordinator demo-c1 (scope: auth)  workers: 2", "demo-1", "root-owned workers: 1", "demo-9"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q missing %q", out, want)
		}
	}
}

func TestCoordinatorListRequiresAProjectAndExitsTwo(t *testing.T) {
	cfg := setConfigEnv(t)
	_ = cfg
	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "coordinator", "ls")
	if err == nil || ExitCode(err) != 2 {
		t.Fatalf("err=%v exit=%d, want usage error 2", err, ExitCode(err))
	}
}

func TestRemoteWorkerAssignSendsTheLinkOwner(t *testing.T) {
	srv, capture := ownershipServer(t, http.StatusOK,
		`{"hostId":"host-build","remoteSessionId":"widgets-3","projectId":"demo","ownerSessionId":"demo-c1"}`)
	t.Setenv("AO_SESSION_ID", "demo-root")

	out, errOut, err := runOwnershipCLI(t, srv, "remote-worker", "assign", "host-build", "widgets-3", "--owner", "demo-c1")
	if err != nil {
		t.Fatalf("remote-worker assign failed: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodPut || capture.path != "/api/v1/remote-worker-links/host-build/widgets-3/owner" {
		t.Fatalf("request = %s %s", capture.method, capture.path)
	}
	var body sessionOwnerRequest
	if err := json.Unmarshal([]byte(capture.body), &body); err != nil {
		t.Fatalf("body %q: %v", capture.body, err)
	}
	if body.OwnerSessionID == nil || *body.OwnerSessionID != "demo-c1" || body.RequestedBySessionID != "demo-root" {
		t.Fatalf("body = %+v", body)
	}
	if !strings.Contains(out, "remote worker host-build/widgets-3 owner: demo-c1") {
		t.Fatalf("output = %s", out)
	}
}

func TestRemoteWorkerListShowsRootAndCoordinatorOwners(t *testing.T) {
	srv, capture := ownershipServer(t, http.StatusOK,
		`{"links":[{"hostId":"host-build","remoteSessionId":"widgets-3","remoteProjectId":"widgets","projectId":"demo","displayName":"auth","ownerSessionId":"demo-c1"},{"hostId":"host-test","remoteSessionId":"widgets-4","remoteProjectId":"widgets","projectId":"demo","ownerSessionId":null}]}`)

	out, errOut, err := runOwnershipCLI(t, srv, "remote-worker", "ls", "--project", "demo")
	if err != nil {
		t.Fatalf("remote-worker ls failed: %v\nstderr=%s", err, errOut)
	}
	if capture.query != "project=demo" {
		t.Fatalf("query = %q", capture.query)
	}
	for _, want := range []string{"HOST", "host-build", "widgets-3", "demo-c1", "host-test", "root"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q missing %q", out, want)
		}
	}
}

func TestSessionListJSONKeepsNullOwnerForRootOwnedWorker(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/sessions" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("active") == "false" {
			_, _ = io.WriteString(w, `{"sessions":[]}`)
			return
		}
		_, _ = io.WriteString(w, `{"sessions":[
			{"id":"demo-1","projectId":"demo","kind":"worker","ownerSessionId":"demo-c1","activity":{"state":"working","lastActivityAt":"2026-06-02T12:00:00Z"},"isTerminated":false,"createdAt":"2026-06-02T11:00:00Z","updatedAt":"2026-06-02T12:00:00Z","status":"working","prs":[]},
			{"id":"demo-2","projectId":"demo","kind":"worker","ownerSessionId":null,"activity":{"state":"working","lastActivityAt":"2026-06-02T12:00:00Z"},"isTerminated":false,"createdAt":"2026-06-02T11:00:00Z","updatedAt":"2026-06-02T12:00:00Z","status":"working","prs":[]}]}`)
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "session", "ls", "--project", "demo", "--json")
	if err != nil {
		t.Fatalf("session ls --json failed: %v\nstderr=%s", err, errOut)
	}
	var got struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not decodable: %v\n%s", err, out)
	}
	ownerByID := map[string]json.RawMessage{}
	for _, entry := range got.Data {
		id := strings.Trim(string(entry["id"]), `"`)
		owner, ok := entry["ownerSessionId"]
		if !ok {
			t.Fatalf("entry %s has no ownerSessionId key: %s", id, out)
		}
		ownerByID[id] = owner
	}
	if string(ownerByID["demo-1"]) != `"demo-c1"` {
		t.Fatalf("demo-1 owner = %s, want demo-c1", ownerByID["demo-1"])
	}
	if string(ownerByID["demo-2"]) != "null" {
		t.Fatalf("demo-2 owner = %s, want explicit null", ownerByID["demo-2"])
	}
}

func TestSessionListShowsOwnerColumn(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/sessions":
			if r.URL.Query().Get("active") == "false" {
				_, _ = io.WriteString(w, `{"sessions":[]}`)
				return
			}
			_, _ = io.WriteString(w, `{"sessions":[
				{"id":"demo-1","projectId":"demo","kind":"worker","ownerSessionId":"demo-c1","activity":{"state":"working","lastActivityAt":"2026-06-02T12:00:00Z"},"isTerminated":false,"createdAt":"2026-06-02T11:00:00Z","updatedAt":"2026-06-02T12:00:00Z","status":"working","prs":[]},
				{"id":"demo-2","projectId":"demo","kind":"worker","ownerSessionId":null,"activity":{"state":"working","lastActivityAt":"2026-06-02T12:00:00Z"},"isTerminated":false,"createdAt":"2026-06-02T11:00:00Z","updatedAt":"2026-06-02T12:00:00Z","status":"working","prs":[]}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "session", "ls", "--project", "demo")
	if err != nil {
		t.Fatalf("session ls failed: %v\nstderr=%s", err, errOut)
	}
	for _, want := range []string{"OWNER", "demo-c1", "root"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q missing %q", out, want)
		}
	}
}

func TestCoordinatorReportPostsASummaryAsTheCallingCoordinator(t *testing.T) {
	srv, capture := ownershipServer(t, http.StatusCreated, `{"id":"csum_1","state":"blocked","escalation":true,"deliveryState":"pending"}`)
	t.Setenv("AO_SESSION_ID", "demo-coord")

	out, errOut, err := runOwnershipCLI(t, srv, "coordinator", "report", "--state", "blocked", "--escalate", "other",
		"--note", "need a call", "--worker", "demo-1", "--worker", "demo-2", "--result", "stuck on schema", "--submission-key", "key-1")
	if err != nil {
		t.Fatalf("coordinator report failed: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodPost || capture.path != "/api/v1/coordinator-summaries" {
		t.Fatalf("request = %s %s", capture.method, capture.path)
	}
	var body coordinatorSummaryBody
	if err := json.Unmarshal([]byte(capture.body), &body); err != nil {
		t.Fatalf("body %q: %v", capture.body, err)
	}
	if body.CoordinatorSessionID != "demo-coord" || body.SubmissionKey != "key-1" || body.State != "blocked" ||
		body.EscalationReason != "other" || body.EscalationNote != "need a call" || len(body.WorkerSessionIDs) != 2 || body.Result != "stuck on schema" {
		t.Fatalf("body = %+v", body)
	}
	if !strings.Contains(out, "escalation csum_1 queued for the root") {
		t.Fatalf("output = %q", out)
	}
}

func TestCoordinatorReportRequiresStateAndCaller(t *testing.T) {
	srv, _ := ownershipServer(t, http.StatusCreated, `{}`)
	t.Setenv("AO_SESSION_ID", "")
	if _, _, err := runOwnershipCLI(t, srv, "coordinator", "report", "--state", "working"); err == nil || !strings.Contains(err.Error(), "AO_SESSION_ID") {
		t.Fatalf("missing caller err = %v", err)
	}
	t.Setenv("AO_SESSION_ID", "demo-coord")
	if _, _, err := runOwnershipCLI(t, srv, "coordinator", "report"); err == nil || !strings.Contains(err.Error(), "--state") {
		t.Fatalf("missing state err = %v", err)
	}
}
