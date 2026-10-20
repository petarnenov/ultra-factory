package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func serveRemoteSpawn(remote *fakeRemoteHost) {
	serveRemoteProjects(remote, projectDetails{ID: "widgets", Repo: "https://github.com/acme/widgets", Agent: "claude-code"})
	remote.handle(http.MethodPost, "/api/v1/sessions", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, http.StatusCreated, map[string]any{"session": map[string]string{
			"id": "widgets-3", "projectId": "widgets", "status": "spawning", "displayName": "auth refactor",
		}})
	})
}

func TestRemoteSpawnInsideSessionLinksWorkerOnLocalDaemon(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "ao-7")
	t.Setenv("AO_PROJECT_ID", "local-widgets")
	local := startLocalDaemon(t, cfg, "host-local", map[string]projectDetails{
		"local-widgets": {ID: "local-widgets", Repo: "git@github.com:Acme/Widgets.git"},
	})
	local.answerLinks(http.StatusCreated)
	remote := newFakeRemoteHost(t, "host-build", "s3cret")
	serveRemoteSpawn(remote)
	writeRemotesFile(t, remoteEntry{Label: "build-box", URL: remote.srv.URL, Password: "s3cret", HostID: "host-build"})

	_, errOut, err := executeCLI(t, aliveDeps(), "spawn", "--host", "build-box", "--name", "auth refactor", "--prompt", "do it", "--agent", "claude-code", "--skip-agent-check")
	if err != nil {
		t.Fatalf("spawn: %v\nstderr=%s", err, errOut)
	}
	if strings.Contains(errOut, "warning") {
		t.Fatalf("unexpected warning: %s", errOut)
	}
	links := local.links()
	if len(links) != 1 {
		t.Fatalf("link requests on the local daemon = %d, want 1", len(links))
	}
	var got remoteWorkerLinkRequest
	if err := json.Unmarshal([]byte(links[0]), &got); err != nil {
		t.Fatal(err)
	}
	want := remoteWorkerLinkRequest{ParentSessionID: "ao-7", HostID: "host-build", RemoteSessionID: "widgets-3", RemoteProjectID: "widgets", DisplayName: "auth refactor"}
	if got != want {
		t.Fatalf("link = %+v, want %+v", got, want)
	}
	for _, r := range remote.recorded() {
		if r.path == "/api/v1/remote-worker-links" {
			t.Fatal("the link must be recorded on this machine's daemon, not the remote host")
		}
	}
}

// TestRemoteSpawnResolvesHarnessOnTargetMachine asserts `spawn --host` sends
// the target's Project Settings role agent, or its harness default, unmarked.
func TestRemoteSpawnResolvesHarnessOnTargetMachine(t *testing.T) {
	cases := []struct {
		name         string
		project      projectDetails
		args         []string
		wantHarness  string
		wantExplicit bool
		wantModel    string
	}{
		{
			name: "role settings",
			project: projectDetails{ID: "widgets", Repo: "https://github.com/acme/widgets",
				Agent: "codex", Config: &projectConfig{Worker: roleOverride{Agent: "opencode-v2"}}},
			wantHarness: "opencode-v2",
		},
		{
			name:        "machine default",
			project:     projectDetails{ID: "widgets", Repo: "https://github.com/acme/widgets", Agent: "codex"},
			wantHarness: "codex",
		},
		{
			name: "explicit directive",
			project: projectDetails{ID: "widgets", Repo: "https://github.com/acme/widgets",
				Agent: "codex", Config: &projectConfig{Worker: roleOverride{Agent: "opencode-v2"}}},
			args:         []string{"--agent", "opencode"},
			wantHarness:  "opencode",
			wantExplicit: true,
		},
		{
			name: "explicit directive with model",
			project: projectDetails{ID: "widgets", Repo: "https://github.com/acme/widgets",
				Agent: "codex", Config: &projectConfig{Worker: roleOverride{Agent: "opencode-v2"}}},
			args:         []string{"--agent", "opencode-v2", "--model", "opencode-v2/deepseek"},
			wantHarness:  "opencode-v2",
			wantExplicit: true,
			wantModel:    "opencode-v2/deepseek",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AO_SESSION_ID", "")
			remote := newFakeRemoteHost(t, "host-build", "s3cret")
			serveRemoteProjects(remote, tc.project)
			remote.handle(http.MethodPost, "/api/v1/sessions", func(w http.ResponseWriter, _ *http.Request) {
				writeTestJSON(w, http.StatusCreated, map[string]any{"session": map[string]string{
					"id": "widgets-3", "projectId": "widgets", "status": "spawning",
				}})
			})
			writeRemotesFile(t, remoteEntry{Label: "build-box", URL: remote.srv.URL, Password: "s3cret", HostID: "host-build"})

			args := append([]string{"spawn", "--host", "build-box", "--project", "widgets", "--name", "work", "--skip-agent-check"}, tc.args...)
			if _, errOut, err := executeCLI(t, aliveDeps(), args...); err != nil {
				t.Fatalf("spawn: %v\nstderr=%s", err, errOut)
			}
			recorded, ok := remote.find(http.MethodPost, "/api/v1/sessions")
			if !ok {
				t.Fatal("remote spawn request not recorded")
			}
			var spawned spawnRequest
			if err := json.Unmarshal([]byte(recorded.body), &spawned); err != nil {
				t.Fatalf("decode spawn request: %v", err)
			}
			if spawned.Harness != tc.wantHarness {
				t.Fatalf("harness = %q, want %q", spawned.Harness, tc.wantHarness)
			}
			if spawned.Model != tc.wantModel {
				t.Fatalf("model = %q, want %q", spawned.Model, tc.wantModel)
			}
			explicit := spawned.HarnessExplicit != nil && *spawned.HarnessExplicit
			if explicit != tc.wantExplicit {
				t.Fatalf("harnessExplicit = %v, want %v", explicit, tc.wantExplicit)
			}
		})
	}
}

func serveRemoteRollback(remote *fakeRemoteHost) {
	remote.handle(http.MethodPost, "/api/v1/sessions/widgets-3/rollback", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, http.StatusOK, map[string]any{"ok": true, "sessionId": "widgets-3", "killed": true})
	})
}

func spawnRemoteInsideSession(t *testing.T, local *localDaemon, remote *fakeRemoteHost) (string, error) {
	t.Helper()
	writeRemotesFile(t, remoteEntry{Label: "build-box", URL: remote.srv.URL, Password: "s3cret", HostID: "host-build"})
	_, errOut, err := executeCLI(t, aliveDeps(), "spawn", "--host", "build-box", "--name", "auth refactor", "--prompt", "do it", "--agent", "claude-code", "--skip-agent-check")
	return errOut, err
}

func newLinkedSpawnFixture(t *testing.T) (*localDaemon, *fakeRemoteHost) {
	t.Helper()
	cfg := setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "ao-7")
	t.Setenv("AO_PROJECT_ID", "local-widgets")
	local := startLocalDaemon(t, cfg, "host-local", map[string]projectDetails{
		"local-widgets": {ID: "local-widgets", Repo: "git@github.com:Acme/Widgets.git"},
	})
	remote := newFakeRemoteHost(t, "host-build", "s3cret")
	serveRemoteSpawn(remote)
	serveRemoteRollback(remote)
	return local, remote
}

func TestRemoteSpawnOverCapacityCreatesNoRemoteWorker(t *testing.T) {
	local, remote := newLinkedSpawnFixture(t)
	local.answerLinks(http.StatusCreated)
	local.rejectCheckWith("COORDINATOR_WORKER_LIMIT_REACHED")

	_, err := spawnRemoteInsideSession(t, local, remote)
	if err == nil || !strings.Contains(err.Error(), "COORDINATOR_WORKER_LIMIT_REACHED") {
		t.Fatalf("err = %v, want COORDINATOR_WORKER_LIMIT_REACHED", err)
	}
	if _, ok := remote.find(http.MethodPost, "/api/v1/sessions"); ok {
		t.Fatal("a remote worker was spawned although the capacity pre-check failed")
	}
	if links := local.links(); len(links) != 0 {
		t.Fatalf("link requests = %v, want none", links)
	}
}

func TestRemoteSpawnTerminatesTheWorkerWhenLinkingHitsCapacity(t *testing.T) {
	for _, code := range []string{"COORDINATOR_WORKER_LIMIT_REACHED", "PROJECT_WORKER_LIMIT_REACHED", "REMOTE_WORKER_LINK_LIMIT", "OWNER_TERMINATED"} {
		t.Run(code, func(t *testing.T) {
			local, remote := newLinkedSpawnFixture(t)
			local.rejectLinksWith(http.StatusConflict, code)

			errOut, err := spawnRemoteInsideSession(t, local, remote)
			if err == nil || !strings.Contains(err.Error(), code) || !strings.Contains(err.Error(), "widgets-3") {
				t.Fatalf("err = %v, want %s naming widgets-3", err, code)
			}
			if strings.Contains(errOut, "warning") {
				t.Fatalf("unexpected warning: %s", errOut)
			}
			if _, ok := remote.find(http.MethodPost, "/api/v1/sessions/widgets-3/rollback"); !ok {
				t.Fatal("the remote session was not terminated")
			}
		})
	}
}

func TestRemoteSpawnKeepsTheWorkerOnANonCapacityLinkFailure(t *testing.T) {
	local, remote := newLinkedSpawnFixture(t)
	local.rejectLinksWith(http.StatusConflict, "PARENT_SESSION_TERMINATED")

	_, err := spawnRemoteInsideSession(t, local, remote)
	if err == nil || !strings.Contains(err.Error(), "widgets-3") || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("err = %v, want a failure naming the running widgets-3", err)
	}
	if _, ok := remote.find(http.MethodPost, "/api/v1/sessions/widgets-3/rollback"); ok {
		t.Fatal("a non-capacity link failure must keep the remote worker")
	}
}

func TestRemoteSpawnOutsideSessionDoesNotLink(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "")
	local := startLocalDaemon(t, cfg, "host-local", nil)
	local.answerLinks(http.StatusCreated)
	remote := newFakeRemoteHost(t, "host-build", "s3cret")
	serveRemoteSpawn(remote)
	writeRemotesFile(t, remoteEntry{Label: "build-box", URL: remote.srv.URL, Password: "s3cret", HostID: "host-build"})

	if _, errOut, err := executeCLI(t, aliveDeps(), "spawn", "--host", "build-box", "--project", "widgets", "--name", "auth refactor", "--prompt", "do it", "--agent", "claude-code", "--skip-agent-check"); err != nil {
		t.Fatalf("spawn: %v\nstderr=%s", err, errOut)
	}
	if links := local.links(); len(links) != 0 {
		t.Fatalf("spawn outside an AO session linked the worker: %v", links)
	}
}

func TestRemoteSpawnWarnsWhenTheDaemonWillNotPollTheHost(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "ao-7")
	t.Setenv("AO_PROJECT_ID", "local-widgets")
	local := startLocalDaemon(t, cfg, "host-local", map[string]projectDetails{
		"local-widgets": {ID: "local-widgets", Repo: "git@github.com:Acme/Widgets.git"},
	})
	local.answerLinks(http.StatusCreated)
	local.mu.Lock()
	local.linkPullBlocked = "plain http to 192.168.1.223 would send the saved password with no one acting"
	local.mu.Unlock()
	remote := newFakeRemoteHost(t, "host-build", "s3cret")
	serveRemoteSpawn(remote)
	writeRemotesFile(t, remoteEntry{Label: "build-box", URL: remote.srv.URL, Password: "s3cret", HostID: "host-build"})

	_, errOut, err := executeCLI(t, aliveDeps(), "spawn", "--host", "build-box", "--name", "auth refactor", "--prompt", "do it", "--agent", "claude-code", "--skip-agent-check")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if !strings.Contains(errOut, "warning: reports from widgets-3 will not reach this machine's orchestrator: plain http to 192.168.1.223") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestRemoteSpawnMarksTheWorkerAsRemote(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "")
	startLocalDaemon(t, cfg, "host-local", nil)
	remote := newFakeRemoteHost(t, "host-build", "s3cret")
	serveRemoteSpawn(remote)
	writeRemotesFile(t, remoteEntry{Label: "build-box", URL: remote.srv.URL, Password: "s3cret", HostID: "host-build"})

	if _, errOut, err := executeCLI(t, aliveDeps(), "spawn", "--host", "build-box", "--project", "widgets", "--name", "auth refactor", "--prompt", "do it", "--agent", "claude-code", "--skip-agent-check"); err != nil {
		t.Fatalf("spawn: %v\nstderr=%s", err, errOut)
	}
	req, ok := remote.find(http.MethodPost, "/api/v1/sessions")
	if !ok {
		t.Fatal("spawn request did not reach the remote host")
	}
	var got spawnRequest
	if err := json.Unmarshal([]byte(req.body), &got); err != nil {
		t.Fatal(err)
	}
	if !got.RemoteSpawn {
		t.Fatalf("remote worker spawn body = %s, want remoteSpawn true", req.body)
	}
}

func TestRemoteOrchestratorSpawnIsNotMarkedRemote(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "")
	startLocalDaemon(t, cfg, "host-local", nil)
	remote := newFakeRemoteHost(t, "host-build", "s3cret")
	serveRemoteSpawn(remote)
	writeRemotesFile(t, remoteEntry{Label: "build-box", URL: remote.srv.URL, Password: "s3cret", HostID: "host-build"})

	if _, errOut, err := executeCLI(t, aliveDeps(), "spawn", "--host", "build-box", "--project", "widgets", "--kind", "orchestrator", "--name", "coord", "--agent", "claude-code", "--skip-agent-check"); err != nil {
		t.Fatalf("spawn: %v\nstderr=%s", err, errOut)
	}
	req, ok := remote.find(http.MethodPost, "/api/v1/sessions")
	if !ok {
		t.Fatal("spawn request did not reach the remote host")
	}
	if strings.Contains(req.body, "remoteSpawn") {
		t.Fatalf("orchestrator spawn body = %s, want no remoteSpawn", req.body)
	}
}
