package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// storedCoordinatorProject is the project the merge tests start from: other
// config fields and a coordinator block that the flags must not disturb.
const storedCoordinatorProject = `{"status":"ok","project":{"id":"demo","name":"Demo","config":{"defaultBranch":"main","sessionPrefix":"dm","coordinators":{"enabled":true,"maxWorkers":6,"agentProfiles":[{"id":"fast","harness":"codex","model":"m","effort":"low"}],"allowLocalWorkers":true}}}}`

func TestProjectSetConfigMergeKeepsStoredFields(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, storedCoordinatorProject)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
		"project", "set-config", "demo", "--merge", "--max-workers-per-coordinator", "4")
	if err != nil {
		t.Fatalf("set-config --merge: %v\nstderr=%s", err, errOut)
	}
	var got setConfigRequest
	if err := json.Unmarshal(capture.body, &got); err != nil {
		t.Fatalf("decode %q: %v", capture.body, err)
	}
	if got.Config.DefaultBranch != "main" || got.Config.SessionPrefix != "dm" {
		t.Fatalf("non-coordinator fields changed: %+v", got.Config)
	}
	want := coordinatorsConfig{
		Enabled:                  true,
		MaxWorkers:               6,
		MaxWorkersPerCoordinator: 4,
		AgentProfiles:            []agentProfile{{ID: "fast", Harness: "codex", Model: "m", Effort: "low"}},
		AllowLocalWorkers:        true,
	}
	if !reflect.DeepEqual(got.Config.Coordinators, want) {
		t.Fatalf("coordinators = %+v, want %+v", got.Config.Coordinators, want)
	}
}

func TestProjectSetConfigMergeOverlaysTheFlagsPassed(t *testing.T) {
	cfg := setConfigEnv(t)
	writeRemotesFile(t, remoteEntry{Label: "Linux", URL: "http://linux.invalid:3001", Password: "p", HostID: "host-linux-id"})
	srv, capture := projectServer(t, http.StatusOK, storedCoordinatorProject)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
		"project", "set-config", "demo", "--merge", "--allowed-hosts", "Linux", "--no-allow-local-workers", "--coordinators=false")
	if err != nil {
		t.Fatalf("set-config --merge: %v\nstderr=%s", err, errOut)
	}
	var got setConfigRequest
	if err := json.Unmarshal(capture.body, &got); err != nil {
		t.Fatalf("decode %q: %v", capture.body, err)
	}
	c := got.Config.Coordinators
	if c.Enabled || c.AllowLocalWorkers || !reflect.DeepEqual(c.AllowedHosts, []string{"host-linux-id"}) {
		t.Fatalf("coordinators = %+v", c)
	}
	if c.MaxWorkers != 6 || len(c.AgentProfiles) != 1 {
		t.Fatalf("unpassed fields changed: %+v", c)
	}
}

func TestProjectSetConfigMergeUsageErrorsExitTwo(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no coordinator flag", []string{"--merge"}, "--merge needs at least one coordinator flag"},
		{"other field flag", []string{"--merge", "--max-workers", "2", "--default-branch", "trunk"}, "--merge applies to coordinator flags only"},
		{"config json", []string{"--merge", "--max-workers", "2", "--config-json", "{}"}, "--merge applies to coordinator flags only"},
		{"clear", []string{"--merge", "--max-workers", "2", "--clear"}, "--merge applies to coordinator flags only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := setConfigEnv(t)
			srv, capture := projectServer(t, http.StatusOK, storedCoordinatorProject)
			writeRunFileFor(t, cfg, srv)

			_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
				append([]string{"project", "set-config", "demo"}, tc.args...)...)
			var usage usageError
			if !errors.As(err, &usage) || ExitCode(err) != 2 || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v exit=%d, want usage error containing %q", err, ExitCode(err), tc.want)
			}
			if capture.method == http.MethodPut {
				t.Fatalf("config was written: %s", capture.body)
			}
		})
	}
}

func TestProjectSetConfigAllowedHostsAndLocalWorkersRoundTrip(t *testing.T) {
	cfg := setConfigEnv(t)
	writeRemotesFile(t,
		remoteEntry{Label: "Linux", URL: "http://linux.invalid:3001", Password: "p", HostID: "host-linux-id"},
		remoteEntry{Label: "Z600", URL: "http://z600.invalid:3001", Password: "p", HostID: "host-z600-id"},
	)
	srv, capture := projectServer(t, http.StatusOK, `{"status":"ok","project":{"id":"demo"}}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
		"project", "set-config", "demo", "--coordinators", "--allowed-hosts", "Linux,host-z600-id", "--allow-local-workers")
	if err != nil {
		t.Fatalf("set-config: %v\nstderr=%s", err, errOut)
	}
	var got setConfigRequest
	if err := json.Unmarshal(capture.body, &got); err != nil {
		t.Fatalf("decode %q: %v", capture.body, err)
	}
	if !reflect.DeepEqual(got.Config.Coordinators.AllowedHosts, []string{"host-linux-id", "host-z600-id"}) || !got.Config.Coordinators.AllowLocalWorkers {
		t.Fatalf("coordinators = %+v", got.Config.Coordinators)
	}
}

func TestProjectSetConfigLocalWorkerFlagsAreExclusiveAndHostsMustResolve(t *testing.T) {
	cfg := setConfigEnv(t)
	writeRemotesFile(t, remoteEntry{Label: "Linux", URL: "http://linux.invalid:3001", Password: "p", HostID: "host-linux-id"})
	srv, capture := projectServer(t, http.StatusOK, `{"status":"ok","project":{"id":"demo"}}`)
	writeRunFileFor(t, cfg, srv)

	for name, args := range map[string][]string{
		"both local flags": {"--allow-local-workers", "--no-allow-local-workers"},
		"unknown host":     {"--allowed-hosts", "Mars"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
				append([]string{"project", "set-config", "demo", "--coordinators"}, args...)...)
			if ExitCode(err) != 2 {
				t.Fatalf("exit = %d (err %v), want usage error 2", ExitCode(err), err)
			}
			if capture.method == http.MethodPut {
				t.Fatalf("config was written: %s", capture.body)
			}
		})
	}
}

// TestProjectGetReadsAnEmptyAgentModeAsInherit keeps #502's stored config
// readable: an empty or missing mode is inherit, not a mode echo.
func TestProjectGetReadsAnEmptyAgentModeAsInherit(t *testing.T) {
	cfg := setConfigEnv(t)
	stored := `{"status":"ok","project":{"id":"demo","config":{"coordinators":{"enabled":true,"coordinatorAgent":{"mode":""},"workerAgent":{},"rootWorkerAgent":{"mode":"inherit"}}}}}`
	srv, _ := projectServer(t, http.StatusOK, stored)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "project", "get", "demo")
	if err != nil {
		t.Fatalf("project get: %v\nstderr=%s", err, errOut)
	}
	if strings.Contains(out, `"mode":""`) {
		t.Fatalf("project get kept an empty mode: %s", out)
	}
	for _, want := range []string{
		`"coordinatorAgent":{"mode":"inherit"}`,
		`"workerAgent":{"mode":"inherit"}`,
		`"rootWorkerAgent":{"mode":"inherit"}`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("project get = %s, want %s", out, want)
		}
	}
}

// TestProjectSetConfigMergeWritesNoEmptyAgentMode covers the producer: a merge
// that sets a selection never writes {"mode":""}.
func TestProjectSetConfigMergeWritesNoEmptyAgentMode(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, storedCoordinatorProject)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
		"project", "set-config", "demo", "--merge", "--coordinator-agent", "inherit")
	if err != nil {
		t.Fatalf("set-config --merge: %v\nstderr=%s", err, errOut)
	}
	var body struct {
		Config struct {
			Coordinators map[string]json.RawMessage `json:"coordinators"`
		} `json:"config"`
	}
	if err := json.Unmarshal(capture.body, &body); err != nil {
		t.Fatalf("decode %q: %v", capture.body, err)
	}
	if got := string(body.Config.Coordinators["coordinatorAgent"]); got != `{"mode":"inherit"}` {
		t.Fatalf("coordinatorAgent = %s, want {\"mode\":\"inherit\"} (body %s)", got, capture.body)
	}
	if strings.Contains(string(capture.body), `"mode":""`) {
		t.Fatalf("merge wrote an empty mode: %s", capture.body)
	}
}

// TestProjectSetConfigMergePreservesFieldsTheCLIDoesNotModel guards the raw
// merge: stored preRemove, aggregateProjects and unknown keys stay unchanged.
func TestProjectSetConfigMergePreservesFieldsTheCLIDoesNotModel(t *testing.T) {
	cfg := setConfigEnv(t)
	stored := `{"status":"ok","project":{"id":"demo","config":{"preRemove":["echo bye"],"aggregateProjects":[{"hostId":"host-linux-id","projectId":"mercury"}],"futureField":{"keep":true},"coordinators":{"enabled":true,"maxWorkers":6,"customCoordinatorKey":"kept"}}}}`
	srv, capture := projectServer(t, http.StatusOK, stored)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
		"project", "set-config", "demo", "--merge", "--max-workers", "2")
	if err != nil {
		t.Fatalf("set-config --merge: %v\nstderr=%s", err, errOut)
	}
	var got struct {
		Config map[string]json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(capture.body, &got); err != nil {
		t.Fatalf("decode %q: %v", capture.body, err)
	}
	for key, want := range map[string]string{
		"preRemove":         `["echo bye"]`,
		"aggregateProjects": `[{"hostId":"host-linux-id","projectId":"mercury"}]`,
		"futureField":       `{"keep":true}`,
	} {
		if string(got.Config[key]) != want {
			t.Fatalf("%s = %s, want %s (body %s)", key, got.Config[key], want, capture.body)
		}
	}
	var coordinators map[string]json.RawMessage
	if err := json.Unmarshal(got.Config["coordinators"], &coordinators); err != nil {
		t.Fatalf("coordinators: %v", err)
	}
	if string(coordinators["customCoordinatorKey"]) != `"kept"` || string(coordinators["maxWorkers"]) != "2" || string(coordinators["enabled"]) != "true" {
		t.Fatalf("coordinators = %s", got.Config["coordinators"])
	}
}
