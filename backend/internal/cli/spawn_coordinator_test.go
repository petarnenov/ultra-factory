package cli

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSpawnCoordinatorUsageErrorsExitTwoWithoutContactingTheDaemon(t *testing.T) {
	long := strings.Repeat("s", 201)
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"missing scope", []string{"--kind", "coordinator"}, "--scope is required with --kind coordinator"},
		{"blank scope", []string{"--kind", "coordinator", "--scope", "  "}, "--scope is required with --kind coordinator"},
		{"scope too long", []string{"--kind", "coordinator", "--scope", long}, "--scope must be 200 characters or fewer"},
		{"scope on a worker", []string{"--scope", "auth"}, "--scope is supported with --kind coordinator only"},
		{"scope on an orchestrator", []string{"--kind", "orchestrator", "--scope", "auth"}, "--scope is supported with --kind coordinator only"},
		{"standalone", []string{"--kind", "coordinator", "--scope", "auth", "--standalone", "--agent", "codex"}, "standalone sessions must be workers"},
		{"artifact output", []string{"--kind", "coordinator", "--scope", "auth", "--output", "artifact"}, "--output is supported for worker sessions only"},
		{"claim pr", []string{"--kind", "coordinator", "--scope", "auth", "--claim-pr", "12"}, "--claim-pr is supported for worker sessions only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"spawn", "--name", "coord"}
			if tc.name != "standalone" {
				args = append(args, "--project", "demo")
			}
			args = append(args, tc.args...)
			_, _, err := executeCLI(t, Deps{}, args...)
			if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v exit=%d, want usage error containing %q", err, ExitCode(err), tc.want)
			}
		})
	}
}

func TestValidateCoordinatorSpawnRejectsRemoteTargets(t *testing.T) {
	opts := spawnOptions{kind: "coordinator", scope: "auth"}
	err := validateCoordinatorSpawn(opts, true)
	var usage usageError
	if !errors.As(err, &usage) || !strings.Contains(err.Error(), "--host is not supported with --kind coordinator") {
		t.Fatalf("err = %v, want a usage error", err)
	}
	if err := validateCoordinatorSpawn(opts, false); err != nil {
		t.Fatalf("local coordinator spawn: %v", err)
	}
	if err := validateCoordinatorSpawn(spawnOptions{kind: "worker"}, true); err != nil {
		t.Fatalf("workers keep --host: %v", err)
	}
}

func TestSpawnCoordinatorSendsScopeAndTheCallingSessionAsParent(t *testing.T) {
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

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
		"spawn", "--project", "demo", "--agent", "codex", "--skip-agent-check", "--name", "auth", "--kind", "coordinator", "--scope", "auth module")
	if err != nil {
		t.Fatalf("spawn failed: %v stderr=%s", err, errOut)
	}
	if req.Kind != "coordinator" || req.Scope != "auth module" || req.ParentSessionID != "demo-1" || req.RemoteSpawn {
		t.Fatalf("spawn request = %#v", req)
	}
	if !strings.Contains(out, "spawned session demo-2") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestSpawnCoordinatorCapacityErrorExitsOneWithTheEnvelope(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/demo":
			_, _ = io.WriteString(w, `{"status":"ok","project":{"id":"demo","name":"Demo","path":"/repo/demo"}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions":
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":"conflict","message":"The project already has the maximum number of active coordinators","code":"COORDINATOR_LIMIT_REACHED","requestId":"req-cap","details":{"projectId":"demo","current":2,"allowed":2}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
		"spawn", "--project", "demo", "--agent", "codex", "--skip-agent-check", "--name", "auth", "--kind", "coordinator", "--scope", "auth")
	if err == nil || ExitCode(err) != 1 || !strings.Contains(err.Error(), "(COORDINATOR_LIMIT_REACHED) [request req-cap]") {
		t.Fatalf("err=%v exit=%d, want the daemon envelope and exit 1", err, ExitCode(err))
	}
}
