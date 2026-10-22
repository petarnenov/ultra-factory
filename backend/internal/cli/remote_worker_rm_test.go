package cli

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestRemoteWorkerRemoveSendsTheRootCallerAndPrintsTheRemoval(t *testing.T) {
	srv, capture := ownershipServer(t, http.StatusOK,
		`{"hostId":"host-build","hostLabel":"build-box","remoteSessionId":"widgets-3","remoteProjectId":"widgets","projectId":"demo","ownerSessionId":null,"source":"spawn","createdAt":"2026-10-22T00:00:00Z","state":"stale"}`)
	t.Setenv("AO_SESSION_ID", "demo-root")

	out, _, err := runOwnershipCLI(t, srv, "remote-worker", "rm", "host-build", "widgets-3")
	if err != nil {
		t.Fatal(err)
	}
	if capture.method != http.MethodDelete || capture.path != "/api/v1/remote-worker-links/host-build/widgets-3" {
		t.Fatalf("request = %s %s", capture.method, capture.path)
	}
	if capture.query != "requestedBySessionId=demo-root" {
		t.Fatalf("query = %q, want the caller named", capture.query)
	}
	if !strings.Contains(out, "remote worker host-build/widgets-3 removed") {
		t.Fatalf("output = %q", out)
	}
}

func TestRemoteWorkerRemoveRequiresACallerAndTwoArguments(t *testing.T) {
	srv, _ := ownershipServer(t, http.StatusOK, `{}`)
	t.Setenv("AO_SESSION_ID", "")
	_, _, err := runOwnershipCLI(t, srv, "remote-worker", "rm", "host-build", "widgets-3")
	var usage usageError
	if !errors.As(err, &usage) {
		t.Fatalf("missing caller: err = %v, want a usage error", err)
	}
	_, _, err = runOwnershipCLI(t, srv, "remote-worker", "rm", "host-build")
	if !errors.As(err, &usage) {
		t.Fatalf("one argument: err = %v, want a usage error", err)
	}
}

func TestRemoteWorkerListShowsTheLinkState(t *testing.T) {
	srv, _ := ownershipServer(t, http.StatusOK,
		`{"links":[{"hostId":"host-build","remoteSessionId":"widgets-3","remoteProjectId":"widgets","projectId":"demo","ownerSessionId":null,"source":"spawn","state":"stale"},{"hostId":"host-test","remoteSessionId":"widgets-4","remoteProjectId":"widgets","projectId":"demo","ownerSessionId":null,"source":"spawn","state":"live"}]}`)

	out, _, err := runOwnershipCLI(t, srv, "remote-worker", "ls")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "STATE") || !strings.Contains(out, "stale") || !strings.Contains(out, "live") {
		t.Fatalf("output = %q", out)
	}
}

func TestSessionKillOnAHostDropsItsLinkOnTheLocalDaemon(t *testing.T) {
	cfg := setConfigEnv(t)
	local := startLocalDaemon(t, cfg, "host-local", nil)
	remote := newFakeRemoteHost(t, "host-build", "s3cret")
	remote.handle(http.MethodPost, "/api/v1/sessions/widgets-3/kill", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, http.StatusOK, map[string]any{"sessionId": "widgets-3", "freed": true})
	})
	writeRemotesFile(t, remoteEntry{Label: "build-box", URL: remote.srv.URL, Password: "s3cret", HostID: "host-build"})

	out, errOut, err := executeCLI(t, aliveDeps(), "session", "kill", "widgets-3", "--host", "build-box")
	if err != nil {
		t.Fatalf("kill: %v\nstderr=%s", err, errOut)
	}
	if !strings.Contains(out, "killed") || strings.Contains(errOut, "warning") {
		t.Fatalf("out = %q, stderr = %q", out, errOut)
	}
	if !local.saw("POST /api/v1/remote-worker-links/host-build/widgets-3/release") {
		t.Fatalf("the local daemon was not asked to drop the link; saw %v", local.paths)
	}
}
