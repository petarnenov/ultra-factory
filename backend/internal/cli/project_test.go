package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type projectCapture struct {
	method string
	path   string
	body   []byte
}

func projectServer(t *testing.T, status int, respBody string) (*httptest.Server, *projectCapture) {
	t.Helper()
	capture := &projectCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.method = r.Method
		capture.path = r.URL.Path
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		capture.body = data
		if !strings.HasPrefix(r.URL.Path, "/api/v1/projects") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	return srv, capture
}

func TestProjectAdd_GitHubAccountBindsProjectConfig(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"project":{"id":"demo","path":"/repo/demo"}}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "add", "--path", "/repo/demo", "--github-account", "work-account")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodPost || capture.path != "/api/v1/projects" {
		t.Fatalf("request = %s %s, want POST /api/v1/projects", capture.method, capture.path)
	}
	var got addProjectRequest
	if err := json.Unmarshal(capture.body, &got); err != nil {
		t.Fatalf("decode request: %v\nbody=%s", err, capture.body)
	}
	if got.Config == nil || got.Config.SCM.GitHub.Account != "work-account" {
		t.Fatalf("config = %#v, want github account", got.Config)
	}
	if strings.Contains(string(capture.body), "ghp_") {
		t.Fatal("project request contains a token-like value")
	}
}

func TestProjectSetConfig_GitHubAccountFlagAndClear(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"project":{"id":"demo","path":"/repo/demo"}}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "set-config", "demo", "--github-account", "work-account")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	var got setConfigRequest
	if err := json.Unmarshal(capture.body, &got); err != nil {
		t.Fatalf("decode request: %v\nbody=%s", err, capture.body)
	}
	if got.Config.SCM.GitHub.Account != "work-account" {
		t.Fatalf("config = %#v, want github account", got.Config)
	}

	_, errOut, err = executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "set-config", "demo", "--github-account", "")
	if err != nil {
		t.Fatalf("clear: unexpected error: %v\nstderr=%s", err, errOut)
	}
	got = setConfigRequest{}
	if err := json.Unmarshal(capture.body, &got); err != nil {
		t.Fatalf("decode request: %v\nbody=%s", err, capture.body)
	}
	if got.Config.SCM.GitHub.Account != "" {
		t.Fatalf("config = %#v, want cleared github account", got.Config)
	}
}

func TestProjectSetConfig_TrackerIntakeFlags(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"project":{"id":"demo","path":"/repo/demo"}}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "set-config", "demo", "--tracker-intake", "--tracker-repo", "acme/demo", "--tracker-assignee", "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodPut || capture.path != "/api/v1/projects/demo/config" {
		t.Fatalf("request = %s %s, want PUT /api/v1/projects/demo/config", capture.method, capture.path)
	}
	var got setConfigRequest
	if err := json.Unmarshal(capture.body, &got); err != nil {
		t.Fatalf("decode request: %v\nbody=%s", err, capture.body)
	}
	// Provider is omitted: the daemon infers it from the project's git origin.
	if !got.Config.TrackerIntake.Enabled || got.Config.TrackerIntake.Provider != "" || got.Config.TrackerIntake.Repo != "acme/demo" || got.Config.TrackerIntake.Assignee != "alice" {
		t.Fatalf("tracker intake request = %#v", got.Config.TrackerIntake)
	}
}

func TestProjectSetConfig_TrackerIntakeJSON(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"project":{"id":"demo","path":"/repo/demo"}}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "set-config", "demo", "--config-json", `{"worker":{"agent":"amp","agentConfig":{"mode":"ultra"}},"trackerIntake":{"enabled":true,"provider":"github","assignee":"alice"}}`)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	var got setConfigRequest
	if err := json.Unmarshal(capture.body, &got); err != nil {
		t.Fatalf("decode request: %v\nbody=%s", err, capture.body)
	}
	if !got.Config.TrackerIntake.Enabled || got.Config.TrackerIntake.Provider != "github" || got.Config.TrackerIntake.Assignee != "alice" {
		t.Fatalf("tracker intake request = %#v", got.Config.TrackerIntake)
	}
	if got.Config.Worker.Agent != "amp" || got.Config.Worker.AgentConfig.Mode != "ultra" {
		t.Fatalf("worker config = %#v, want preserved amp ultra mode", got.Config.Worker)
	}
}

func TestBuildProjectConfigTrackerIntakeFlags(t *testing.T) {
	got, err := buildProjectConfig(projectSetConfigOptions{
		trackerIntake:   true,
		trackerRepo:     "acme/demo",
		trackerAssignee: "alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Provider is omitted: the daemon infers it from the project's git origin.
	if !got.TrackerIntake.Enabled || got.TrackerIntake.Provider != "" || got.TrackerIntake.Repo != "acme/demo" || got.TrackerIntake.Assignee != "alice" {
		t.Fatalf("tracker intake config = %#v", got.TrackerIntake)
	}
}

func TestProjectList_Success(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"projects":[{"id":"zeta","name":"Zeta","sessionPrefix":"zeta"},{"id":"alpha","name":"Alpha","sessionPrefix":"alpha","resolveError":"config missing"}]}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "ls")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodGet || capture.path != "/api/v1/projects" {
		t.Fatalf("request = %s %s, want GET /api/v1/projects", capture.method, capture.path)
	}
	if !strings.Contains(out, "ID") || !strings.Contains(out, "SESSION PREFIX") {
		t.Fatalf("output missing table header:\n%s", out)
	}
	if strings.Index(out, "alpha") > strings.Index(out, "zeta") {
		t.Fatalf("projects should be sorted by id in output:\n%s", out)
	}
	if !strings.Contains(out, "degraded: config missing") {
		t.Fatalf("output missing degraded status:\n%s", out)
	}
}

func TestProjectList_JSON(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := projectServer(t, http.StatusOK, `{"projects":[{"id":"demo","name":"Demo","sessionPrefix":"demo"}]}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "ls", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	var got projectListResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode json output: %v\nout=%s", err, out)
	}
	if len(got.Projects) != 1 || got.Projects[0].ID != "demo" {
		t.Fatalf("projects = %#v, want demo", got.Projects)
	}
}

func TestProjectList_Empty(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := projectServer(t, http.StatusOK, `{"projects":[]}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "ls")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if !strings.Contains(out, "No projects registered") || !strings.Contains(out, "ao project add --path") {
		t.Fatalf("empty output missing hint:\n%s", out)
	}
}

func TestProjectGet_Success(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"status":"ok","project":{"id":"demo","name":"Demo","path":"/repo/demo","repo":"git@example.com:demo.git","defaultBranch":"main","agent":"codex"}}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "get", "demo")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodGet || capture.path != "/api/v1/projects/demo" {
		t.Fatalf("request = %s %s, want GET /api/v1/projects/demo", capture.method, capture.path)
	}
	for _, want := range []string{"Project demo (ok)", "name: Demo", "path: /repo/demo", "default branch: main", "agent: codex"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestProjectGet_JSON(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"status":"degraded","project":{"id":"demo","name":"Demo","path":"/repo/demo","resolveError":"config missing","config":{"worker":{"agent":"amp","agentConfig":{"mode":"high"}}}}}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "get", "demo", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodGet || capture.path != "/api/v1/projects/demo" {
		t.Fatalf("request = %s %s, want GET /api/v1/projects/demo", capture.method, capture.path)
	}
	var got projectGetResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode json output: %v\nout=%s", err, out)
	}
	if got.Status != "degraded" || got.Project.ID != "demo" || got.Project.ResolveError != "config missing" {
		t.Fatalf("get json = %#v, want degraded demo with resolve error", got)
	}
	if got.Project.Config == nil || got.Project.Config.Worker.AgentConfig.Mode != "high" {
		t.Fatalf("get json worker config = %#v, want preserved amp high mode", got.Project.Config)
	}
}

func TestProjectGet_MissingArg(t *testing.T) {
	setConfigEnv(t)
	_, _, err := executeCLI(t, Deps{}, "project", "get")
	if err == nil {
		t.Fatal("expected missing arg error")
	}
	if got := ExitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
}

func TestProjectGet_NotFound(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := projectServer(t, http.StatusNotFound, `{"error":"not_found","code":"PROJECT_NOT_FOUND","message":"Unknown project"}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "get", "missing")
	if err == nil {
		t.Fatal("expected not found error")
	}
	if got := ExitCode(err); got != 1 {
		t.Fatalf("exit code = %d, want 1", got)
	}
	if !strings.Contains(err.Error(), "PROJECT_NOT_FOUND") && !strings.Contains(errOut, "PROJECT_NOT_FOUND") {
		t.Fatalf("error did not surface not found envelope: %v\nstderr=%s", err, errOut)
	}
}

func TestProjectSetConfig_RulesFlags(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"status":"ok","project":{"id":"demo","config":{"agentRules":"Run tests.","agentRulesFile":"docs/rules.md","orchestratorRules":"Delegate."}}}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "set-config", "demo",
		"--agent-rules", "Run tests.",
		"--agent-rules-file", "docs/rules.md",
		"--orchestrator-rules", "Delegate.",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodPut || capture.path != "/api/v1/projects/demo/config" {
		t.Fatalf("request = %s %s, want PUT /api/v1/projects/demo/config", capture.method, capture.path)
	}
	var got setConfigRequest
	if err := json.Unmarshal(capture.body, &got); err != nil {
		t.Fatalf("decode request body: %v\nbody=%s", err, capture.body)
	}
	if got.Config.AgentRules != "Run tests." || got.Config.AgentRulesFile != "docs/rules.md" || got.Config.OrchestratorRules != "Delegate." {
		t.Fatalf("rules config = %#v", got.Config)
	}
	if !strings.Contains(out, "updated config for project demo") {
		t.Fatalf("output missing update message:\n%s", out)
	}
}

func TestProjectSetConfig_ReviewerJSON(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"status":"ok","project":{"id":"demo","config":{"sessionPrefix":"work","reviewers":[{"harness":"claude-code"}]}}}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "set-config", "demo", "--config-json", `{"reviewers":[{"harness":"claude-code"}],"sessionPrefix":"work"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	var got setConfigRequest
	if err := json.Unmarshal(capture.body, &got); err != nil {
		t.Fatalf("decode request body: %v\nbody=%s", err, capture.body)
	}
	if len(got.Config.Reviewers) != 1 || got.Config.Reviewers[0].Harness != "claude-code" {
		t.Fatalf("reviewers config = %#v, want claude-code reviewer preserved", got.Config.Reviewers)
	}
}

func TestProjectSetConfig_ReviewerFlags(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"status":"ok","project":{"id":"demo"}}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "set-config", "demo", "--reviewer", "claude-code")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	var got setConfigRequest
	if err := json.Unmarshal(capture.body, &got); err != nil {
		t.Fatalf("decode request body: %v\nbody=%s", err, capture.body)
	}
	if len(got.Config.Reviewers) != 1 || got.Config.Reviewers[0].Harness != "claude-code" {
		t.Fatalf("reviewers config = %#v, want single claude-code reviewer", got.Config.Reviewers)
	}
}

func TestProjectSetConfig_TerminateOnPRMergeFlag(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"status":"ok","project":{"id":"demo"}}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "set-config", "demo", "--terminate-on-pr-merge")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	var got setConfigRequest
	if err := json.Unmarshal(capture.body, &got); err != nil {
		t.Fatalf("decode request body: %v\nbody=%s", err, capture.body)
	}
	if !got.Config.TerminateOnPRMerge {
		t.Fatalf("terminateOnPrMerge = %v, want true", got.Config.TerminateOnPRMerge)
	}
}

func TestProjectSetConfig_TerminateOnPRMergeConfigJSON(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"status":"ok","project":{"id":"demo"}}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "set-config", "demo", "--config-json", `{"terminateOnPrMerge":true}`)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	var got setConfigRequest
	if err := json.Unmarshal(capture.body, &got); err != nil {
		t.Fatalf("decode request body: %v\nbody=%s", err, capture.body)
	}
	if !got.Config.TerminateOnPRMerge {
		t.Fatalf("terminateOnPrMerge = %v, want true", got.Config.TerminateOnPRMerge)
	}
}

func TestProjectSetConfig_CoordinatorFlags(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"status":"ok","project":{"id":"demo"}}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "set-config", "demo", "--coordinators", "--max-coordinators", "1",
		"--max-workers-per-coordinator", "2", "--max-workers", "4")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	var got setConfigRequest
	if err := json.Unmarshal(capture.body, &got); err != nil {
		t.Fatalf("decode request body: %v\nbody=%s", err, capture.body)
	}
	want := coordinatorsConfig{Enabled: true, MaxCoordinators: 1, MaxWorkersPerCoordinator: 2, MaxWorkers: 4}
	if got.Config.Coordinators != want {
		t.Fatalf("coordinators = %+v, want %+v", got.Config.Coordinators, want)
	}
}

func TestProjectSetConfig_ExplicitZeroLimitIsAccepted(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"status":"ok","project":{"id":"demo"}}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "set-config", "demo", "--max-workers", "0")
	if err != nil {
		t.Fatalf("an explicit zero limit is a valid default: %v\nstderr=%s", err, errOut)
	}
	if len(capture.body) == 0 {
		t.Fatal("the config was not sent")
	}
}

func TestProjectSetConfig_CoordinatorsConfigJSON(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"status":"ok","project":{"id":"demo"}}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "set-config", "demo", "--config-json", `{"coordinators":{"enabled":true,"maxWorkers":5}}`)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	var got setConfigRequest
	if err := json.Unmarshal(capture.body, &got); err != nil {
		t.Fatalf("decode request body: %v\nbody=%s", err, capture.body)
	}
	if !got.Config.Coordinators.Enabled || got.Config.Coordinators.MaxWorkers != 5 {
		t.Fatalf("coordinators = %+v", got.Config.Coordinators)
	}
}

func TestProjectRemove_RequiresID(t *testing.T) {
	setConfigEnv(t)
	_, _, err := executeCLI(t, Deps{}, "project", "rm")
	if err == nil {
		t.Fatal("expected missing id error")
	}
	if got := ExitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
}

func TestProjectRemove_NotFound(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := projectServer(t, http.StatusNotFound, `{"error":"not_found","code":"PROJECT_NOT_FOUND","message":"Unknown project"}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "rm", "missing", "--yes")
	if err == nil {
		t.Fatal("expected not found error")
	}
	if got := ExitCode(err); got != 1 {
		t.Fatalf("exit code = %d, want 1", got)
	}
	if !strings.Contains(err.Error(), "PROJECT_NOT_FOUND") && !strings.Contains(errOut, "PROJECT_NOT_FOUND") {
		t.Fatalf("error did not surface not found envelope: %v\nstderr=%s", err, errOut)
	}
}

func TestProjectRemove_AbortsWhenConfirmationDoesNotMatch(t *testing.T) {
	setConfigEnv(t)
	out, _, err := executeCLI(t, Deps{
		In: strings.NewReader("nope\n"),
	}, "project", "rm", "demo")
	if err != nil {
		t.Fatalf("unexpected abort error: %v", err)
	}
	if !strings.Contains(out, "Type the project id to confirm") || !strings.Contains(out, "aborted") {
		t.Fatalf("output missing prompt/abort:\n%s", out)
	}
}

func TestProjectRemove_DeletesAfterConfirmation(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"ok":true,"id":"demo"}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{
		In:           strings.NewReader("demo\n"),
		ProcessAlive: func(int) bool { return true },
	}, "project", "rm", "demo")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodDelete || capture.path != "/api/v1/projects/demo" {
		t.Fatalf("request = %s %s, want DELETE /api/v1/projects/demo", capture.method, capture.path)
	}
	if !strings.Contains(out, "removed project demo") {
		t.Fatalf("output missing removal message:\n%s", out)
	}
}

func TestProjectRemove_JSONDocumentedEnvelope(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"ok":true,"id":"demo"}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{
		In:           strings.NewReader("wrong\n"),
		ProcessAlive: func(int) bool { return true },
	}, "project", "rm", "demo", "--yes", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodDelete || capture.path != "/api/v1/projects/demo" {
		t.Fatalf("request = %s %s, want DELETE /api/v1/projects/demo", capture.method, capture.path)
	}
	var got projectRemoveResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode json output: %v\nout=%s", err, out)
	}
	if !got.OK || got.ID != "demo" || got.ProjectID != "" {
		t.Fatalf("remove json = %#v, want documented ok/id envelope", got)
	}
}

func TestProjectRemove_JSONBackendEnvelope(t *testing.T) {
	cfg := setConfigEnv(t)
	removedStorageDir := false
	srv, _ := projectServer(t, http.StatusOK, `{"projectId":"demo","removedStorageDir":false}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "rm", "demo", "--yes", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	var got projectRemoveResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode json output: %v\nout=%s", err, out)
	}
	if got.ProjectID != "demo" || got.RemovedStorageDir == nil || *got.RemovedStorageDir != removedStorageDir {
		t.Fatalf("remove json = %#v, want backend projectId/removedStorageDir envelope", got)
	}
}

func TestProjectRemove_EmptySuccessFallsBackToRequestedID(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := projectServer(t, http.StatusNoContent, ``)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "rm", "demo", "--yes")
	if err != nil {
		t.Fatalf("unexpected error for empty 2xx body: %v\nstderr=%s", err, errOut)
	}
	if !strings.Contains(out, "removed project demo") {
		t.Fatalf("output missing fallback removal id:\n%s", out)
	}
}

func TestProjectRemove_YesSkipsConfirmationAndSupportsBackendRemoveEnvelope(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"projectId":"demo","removedStorageDir":false}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{
		In:           strings.NewReader("wrong\n"),
		ProcessAlive: func(int) bool { return true },
	}, "project", "rm", "demo", "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodDelete || capture.path != "/api/v1/projects/demo" {
		t.Fatalf("request = %s %s, want DELETE /api/v1/projects/demo", capture.method, capture.path)
	}
	if strings.Contains(out, "Type the project id") || !strings.Contains(out, "removed project demo") {
		t.Fatalf("--yes output should skip prompt and print removal:\n%s", out)
	}
}
