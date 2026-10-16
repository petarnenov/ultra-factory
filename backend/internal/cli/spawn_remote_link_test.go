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

func TestRemoteSpawnLinkFailureWarnsButKeepsTheWorker(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "ao-7")
	t.Setenv("AO_PROJECT_ID", "local-widgets")
	local := startLocalDaemon(t, cfg, "host-local", map[string]projectDetails{
		"local-widgets": {ID: "local-widgets", Repo: "git@github.com:Acme/Widgets.git"},
	})
	local.answerLinks(http.StatusConflict)
	remote := newFakeRemoteHost(t, "host-build", "s3cret")
	serveRemoteSpawn(remote)
	writeRemotesFile(t, remoteEntry{Label: "build-box", URL: remote.srv.URL, Password: "s3cret", HostID: "host-build"})

	out, errOut, err := executeCLI(t, aliveDeps(), "spawn", "--host", "build-box", "--name", "auth refactor", "--prompt", "do it", "--agent", "claude-code", "--skip-agent-check")
	if err != nil {
		t.Fatalf("a failed link must not fail the spawn: %v", err)
	}
	if !strings.Contains(out, "spawned session widgets-3") {
		t.Fatalf("stdout = %q", out)
	}
	if !strings.Contains(errOut, "warning: reports from widgets-3 will not reach this machine's orchestrator") {
		t.Fatalf("stderr = %q", errOut)
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
