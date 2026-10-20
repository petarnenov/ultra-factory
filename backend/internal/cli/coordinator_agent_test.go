package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const ownershipView = `{"sessionId":"demo-c1","projectId":"demo","kind":"coordinator","ownerSessionId":null,"rootSessionId":"demo-root"}`

func TestCoordinatorSetSendsTheSelectionAndCaller(t *testing.T) {
	for _, tc := range []struct {
		name        string
		session     string
		args        []string
		want        coordinatorWorkerAgentRequest
		wantPrinted string
	}{
		{
			name: "root inside a session", session: "demo-root", args: []string{"--worker-agent", "fixed:a"},
			want: coordinatorWorkerAgentRequest{
				WorkerAgent: agentSelection{Mode: "fixed", ProfileID: "a"}, RequestedBySessionID: "demo-root", Origin: "cli"},
			wantPrinted: "coordinator demo-c1 worker agent: fixed:a",
		},
		{
			name: "operator shell", session: "", args: []string{"--worker-agent", "random:a,b"},
			want: coordinatorWorkerAgentRequest{
				WorkerAgent: agentSelection{Mode: "random", ProfileIDs: []string{"a", "b"}}, OperatorInitiated: true, Origin: "cli"},
		},
		{
			name: "explicit requested-by wins", session: "demo-other", args: []string{"--worker-agent", "inherit", "--requested-by", "demo-root"},
			want: coordinatorWorkerAgentRequest{
				WorkerAgent: agentSelection{Mode: "inherit"}, RequestedBySessionID: "demo-root", Origin: "cli"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, capture := ownershipServer(t, http.StatusOK, ownershipView)
			t.Setenv("AO_SESSION_ID", tc.session)
			out, errOut, err := runOwnershipCLI(t, srv, append([]string{"coordinator", "set", "demo-c1"}, tc.args...)...)
			if err != nil {
				t.Fatalf("coordinator set: %v\nstderr=%s", err, errOut)
			}
			if capture.method != http.MethodPut || capture.path != "/api/v1/sessions/demo-c1/coordinator-worker-agent" {
				t.Fatalf("request = %s %s", capture.method, capture.path)
			}
			var got coordinatorWorkerAgentRequest
			if err := json.Unmarshal([]byte(capture.body), &got); err != nil {
				t.Fatalf("body %q: %v", capture.body, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("body = %+v, want %+v", got, tc.want)
			}
			if tc.wantPrinted != "" && !strings.Contains(out, tc.wantPrinted) {
				t.Fatalf("output = %q", out)
			}
		})
	}
}

func TestCoordinatorSetUsageErrorsExitTwo(t *testing.T) {
	for name, args := range map[string][]string{
		"missing worker agent": {"coordinator", "set", "demo-c1"},
		"bad grammar":          {"coordinator", "set", "demo-c1", "--worker-agent", "shuffle"},
		"fixed without id":     {"coordinator", "set", "demo-c1", "--worker-agent", "fixed:"},
		"missing session":      {"coordinator", "set", "--worker-agent", "inherit"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := setConfigEnv(t)
			_ = cfg
			_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, args...)
			if err == nil || ExitCode(err) != 2 {
				t.Fatalf("err=%v exit=%d, want usage error 2", err, ExitCode(err))
			}
		})
	}
}

func TestCoordinatorSetPassesTheDaemonRefusalThrough(t *testing.T) {
	srv, _ := ownershipServer(t, http.StatusBadRequest,
		`{"error":"invalid","message":"unknown profile","code":"WORKER_PROFILE_INVALID","requestId":"req-1","details":{"field":"workerAgent"}}`)
	t.Setenv("AO_SESSION_ID", "demo-root")
	_, _, err := runOwnershipCLI(t, srv, "coordinator", "set", "demo-c1", "--worker-agent", "fixed:nope")
	if err == nil || ExitCode(err) != 1 || !strings.Contains(err.Error(), "WORKER_PROFILE_INVALID") {
		t.Fatalf("err=%v exit=%d, want exit 1 with WORKER_PROFILE_INVALID", err, ExitCode(err))
	}
}

func TestSpawnCoordinatorSendsAgentSelections(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "demo-1")
	var req spawnRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/demo":
			_, _ = io.WriteString(w, `{"status":"ok","project":{"id":"demo","name":"Demo","path":"/repo/demo"}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions":
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(w, `{"session":{"id":"demo-2","status":"idle"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
		"spawn", "--project", "demo", "--agent", "codex", "--skip-agent-check", "--name", "auth",
		"--kind", "coordinator", "--scope", "auth", "--worker-agent", "random:a,b", "--coordinator-agent", "fixed:c")
	if err != nil {
		t.Fatalf("spawn: %v stderr=%s", err, errOut)
	}
	wantWorker := &agentSelection{Mode: "random", ProfileIDs: []string{"a", "b"}}
	wantCoordinator := &agentSelection{Mode: "fixed", ProfileID: "c"}
	if !reflect.DeepEqual(req.WorkerAgent, wantWorker) || !reflect.DeepEqual(req.CoordinatorAgent, wantCoordinator) {
		t.Fatalf("selections = %+v / %+v", req.WorkerAgent, req.CoordinatorAgent)
	}
}

func TestSpawnAgentSelectionUsageErrors(t *testing.T) {
	for name, args := range map[string][]string{
		"worker-agent on a worker":      {"--worker-agent", "fixed:a"},
		"coordinator-agent on a worker": {"--coordinator-agent", "fixed:a"},
		"bad grammar":                   {"--kind", "coordinator", "--scope", "auth", "--worker-agent", "shuffle"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := executeCLI(t, Deps{}, append([]string{"spawn", "--project", "demo", "--name", "x"}, args...)...)
			if err == nil || ExitCode(err) != 2 {
				t.Fatalf("err=%v exit=%d, want usage error 2", err, ExitCode(err))
			}
		})
	}
}

func TestProjectSetConfigAgentProfileFlags(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"status":"ok","project":{"id":"demo"}}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
		"project", "set-config", "demo",
		"--agent-profile", "id=a,harness=claude-code,model=sonnet,effort=medium,weight=3,label=Fast",
		"--agent-profile", "id=b,harness=codex,model=gpt-5,effort=high",
		"--coordinator-agent", "fixed:a", "--coordinator-worker-agent", "random:a,b", "--root-worker-agent", "inherit")
	if err != nil {
		t.Fatalf("set-config: %v\nstderr=%s", err, errOut)
	}
	var got setConfigRequest
	if err := json.Unmarshal(capture.body, &got); err != nil {
		t.Fatal(err)
	}
	want := coordinatorsConfig{
		AgentProfiles: []agentProfile{
			{ID: "a", Label: "Fast", Harness: "claude-code", Model: "sonnet", Effort: "medium", Weight: 3},
			{ID: "b", Harness: "codex", Model: "gpt-5", Effort: "high"},
		},
		CoordinatorAgent: &agentSelection{Mode: "fixed", ProfileID: "a"},
		WorkerAgent:      &agentSelection{Mode: "random", ProfileIDs: []string{"a", "b"}},
		RootWorkerAgent:  &agentSelection{Mode: "inherit"},
	}
	if !reflect.DeepEqual(got.Config.Coordinators, want) {
		t.Fatalf("coordinators = %+v, want %+v", got.Config.Coordinators, want)
	}
}

func TestProjectSetConfigAgentFlagUsageErrors(t *testing.T) {
	for name, args := range map[string][]string{
		"profile missing effort": {"--agent-profile", "id=a,harness=codex,model=m"},
		"profile bad weight":     {"--agent-profile", "id=a,harness=codex,model=m,effort=e,weight=0"},
		"bad coordinator agent":  {"--coordinator-agent", "fixed"},
		"bad worker agent":       {"--coordinator-worker-agent", "pick:a"},
		"bad root agent":         {"--root-worker-agent", "random:a,"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := setConfigEnv(t)
			srv, capture := projectServer(t, http.StatusOK, `{"status":"ok","project":{"id":"demo"}}`)
			writeRunFileFor(t, cfg, srv)
			_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
				append([]string{"project", "set-config", "demo"}, args...)...)
			if err == nil || ExitCode(err) != 2 {
				t.Fatalf("err=%v exit=%d, want usage error 2", err, ExitCode(err))
			}
			if capture.method == http.MethodPut {
				t.Fatal("a usage error must not reach the daemon")
			}
		})
	}
}

func TestSpawnPreflightSkipsOnlyWhenAProfileWillBePicked(t *testing.T) {
	profiles := `"agentProfiles":[{"id":"a","harness":"codex","model":"m","effort":"e"}]`
	fixedA := `{"mode":"fixed","profileId":"a"}`
	for _, tc := range []struct {
		name          string
		coordinators  string
		session       string
		args          []string
		wantPreflight bool
	}{
		{"inherit selections keep the preflight", profiles, "demo-root", nil, true},
		{"no parent keeps the preflight", profiles + `,"workerAgent":` + fixedA, "", nil, true},
		{"worker from the root with a fixed selection skips it", profiles + `,"rootWorkerAgent":` + fixedA, "demo-root", nil, false},
		{"worker with an explicit agent keeps it", profiles + `,"workerAgent":` + fixedA, "demo-root", []string{"--agent", "codex"}, true},
		{"orchestrator keeps it", profiles + `,"workerAgent":` + fixedA + `,"coordinatorAgent":` + fixedA, "demo-root", []string{"--kind", "orchestrator"}, true},
		{"coordinator with a fixed project selection skips it", profiles + `,"coordinatorAgent":` + fixedA, "demo-root",
			[]string{"--kind", "coordinator", "--scope", "auth"}, false},
		{"coordinator with an inherit project selection keeps it", profiles, "demo-root",
			[]string{"--kind", "coordinator", "--scope", "auth"}, true},
		{"coordinator overriding to a fixed selection skips it", profiles, "demo-root",
			[]string{"--kind", "coordinator", "--scope", "auth", "--coordinator-agent", "fixed:a"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := setConfigEnv(t)
			t.Setenv("AO_SESSION_ID", tc.session)
			preflight := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/demo":
					_, _ = io.WriteString(w, `{"status":"ok","project":{"id":"demo","name":"Demo","path":"/repo/demo","agent":"codex","config":{"coordinators":{`+tc.coordinators+`}}}}`)
				case r.Method == http.MethodPost && r.URL.Path == "/api/v1/agents/readiness/ensure":
					preflight = true
					_, _ = io.WriteString(w, authorizedAgentsJSON("codex"))
				case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions":
					_, _ = io.WriteString(w, `{"session":{"id":"demo-2","status":"idle"}}`)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(srv.Close)
			writeRunFileFor(t, cfg, srv)

			args := append([]string{"spawn", "--project", "demo", "--name", "x"}, tc.args...)
			if _, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, args...); err != nil {
				t.Fatalf("spawn: %v stderr=%s", err, errOut)
			}
			if preflight != tc.wantPreflight {
				t.Fatalf("preflight ran = %v, want %v", preflight, tc.wantPreflight)
			}
		})
	}
}
