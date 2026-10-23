package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestRemoteWorkerSyncPrintsPerHostCountsAndKeepsExitZero(t *testing.T) {
	srv, capture := ownershipServer(t, http.StatusOK,
		`{"hosts":[{"hostId":"host-build","hostLabel":"build-box","linked":2,"revived":1,"skipped":1},{"hostId":"host-down","hostLabel":"down-box","linked":0,"revived":0,"skipped":0,"error":"dial tcp: connection refused"}]}`)

	out, errOut, err := runOwnershipCLI(t, srv, "remote-worker", "sync")
	if err != nil {
		t.Fatalf("remote-worker sync failed on an offline host: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodPost || capture.path != "/api/v1/remote-worker-links/sync" {
		t.Fatalf("request = %s %s", capture.method, capture.path)
	}
	for _, want := range []string{"build-box", "linked 2, revived 1, skipped 1", "down-box", "error: dial tcp: connection refused"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q missing %q", out, want)
		}
	}
}

func TestRemoteWorkerSyncShowsBlockedHosts(t *testing.T) {
	srv, _ := ownershipServer(t, http.StatusOK,
		`{"hosts":[{"hostId":"host-far","hostLabel":"far","linked":0,"skipped":0,"blocked":"plain http needs AO_REMOTE_REPORTS_ALLOW_HTTP"}]}`)

	out, _, err := runOwnershipCLI(t, srv, "remote-worker", "sync")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "linked 0, revived 0, skipped 0, blocked: plain http needs AO_REMOTE_REPORTS_ALLOW_HTTP") {
		t.Fatalf("output = %q", out)
	}
}

func TestRemoteWorkerSyncRejectsArguments(t *testing.T) {
	srv, _ := ownershipServer(t, http.StatusOK, `{"hosts":[]}`)
	_, _, err := runOwnershipCLI(t, srv, "remote-worker", "sync", "extra")
	var usage usageError
	if !errors.As(err, &usage) {
		t.Fatalf("err = %v, want a usage error", err)
	}
}

func TestRemoteWorkerListJSONCarriesTheLocalProjectIDAndPullerID(t *testing.T) {
	srv, _ := ownershipServer(t, http.StatusOK,
		`{"pullerId":"host-local","links":[{"hostId":"host-build","remoteSessionId":"widgets-3","remoteProjectId":"widgets","projectId":"demo","localProjectId":"demo","ownerSessionId":null,"source":"discovered"}]}`)

	out, _, err := runOwnershipCLI(t, srv, "remote-worker", "ls", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		PullerID string `json:"pullerId"`
		Links    []struct {
			LocalProjectID string `json:"localProjectId"`
		} `json:"links"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if len(payload.Links) != 1 || payload.Links[0].LocalProjectID != "demo" {
		t.Fatalf("links = %+v, want localProjectId demo", payload.Links)
	}
	if payload.PullerID != "host-local" {
		t.Fatalf("pullerId = %q, want host-local", payload.PullerID)
	}
}

func TestRemoteWorkerListShowsTheLinkSource(t *testing.T) {
	srv, _ := ownershipServer(t, http.StatusOK,
		`{"links":[{"hostId":"host-build","remoteSessionId":"widgets-3","remoteProjectId":"widgets","projectId":"demo","ownerSessionId":null,"source":"discovered"},{"hostId":"host-test","remoteSessionId":"widgets-4","remoteProjectId":"widgets","projectId":"demo","ownerSessionId":"demo-c1","source":"spawn"}]}`)

	out, _, err := runOwnershipCLI(t, srv, "remote-worker", "ls")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "SOURCE") || !strings.Contains(out, "discovered") || !strings.Contains(out, "spawn") {
		t.Fatalf("output = %q", out)
	}
}
