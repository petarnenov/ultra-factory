package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRemoteHost stands in for another machine's authenticated remote listener:
// identity is the only unauthenticated route, every other request must carry
// the saved bearer password and expected host ID, exactly like authMiddleware
// plus expectedHostGuard.
type fakeRemoteHost struct {
	srv      *httptest.Server
	hostID   string
	password string

	mu       sync.Mutex
	requests []recordedRequest
	handlers map[string]http.HandlerFunc
}

type recordedRequest struct {
	method, path, auth, expectedHost, body string
	tls                                    bool
}

func newFakeRemoteHost(t *testing.T, hostID, password string) *fakeRemoteHost {
	t.Helper()
	return newFakeRemoteHostOn(t, hostID, password, httptest.NewServer)
}

// newFakeRemoteHostTLS is a host reached over https, as through a
// TLS-terminating proxy such as Railway's edge.
func newFakeRemoteHostTLS(t *testing.T, hostID, password string) *fakeRemoteHost {
	t.Helper()
	return newFakeRemoteHostOn(t, hostID, password, httptest.NewTLSServer)
}

func newFakeRemoteHostOn(t *testing.T, hostID, password string, start func(http.Handler) *httptest.Server) *fakeRemoteHost {
	t.Helper()
	h := &fakeRemoteHost{hostID: hostID, password: password, handlers: map[string]http.HandlerFunc{}}
	h.srv = start(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.requests = append(h.requests, recordedRequest{
			method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization"),
			expectedHost: r.Header.Get("X-AO-Expected-Host-ID"), body: string(raw), tls: r.TLS != nil,
		})
		handler := h.handlers[r.Method+" "+r.URL.Path]
		h.mu.Unlock()
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/identity" {
			writeTestJSON(w, http.StatusOK, map[string]any{"hostId": h.hostID, "apiVersion": 1})
			return
		}
		// An empty password models a Tailscale-only host: tailscaled vouches for
		// the caller, and a bearer must not be sent at all.
		if h.password == "" && r.Header.Get("Authorization") != "" {
			writeTestJSON(w, http.StatusForbidden, map[string]string{"message": "unexpected credential", "code": "TAILNET_FORBIDDEN"})
			return
		}
		if h.password != "" && r.Header.Get("Authorization") != "Bearer "+h.password {
			writeTestJSON(w, http.StatusUnauthorized, map[string]string{"message": "missing or invalid connection password", "code": "BAD_PASSWORD"})
			return
		}
		if expected := r.Header.Get("X-AO-Expected-Host-ID"); expected != h.hostID {
			writeTestJSON(w, http.StatusMisdirectedRequest, map[string]string{"code": "HOST_ID_MISMATCH"})
			return
		}
		if handler == nil {
			writeTestJSON(w, http.StatusNotFound, map[string]string{"message": "not found: " + r.Method + " " + r.URL.Path})
			return
		}
		handler(w, r)
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *fakeRemoteHost) handle(method, path string, fn http.HandlerFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers[method+" "+path] = fn
}

func (h *fakeRemoteHost) recorded() []recordedRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]recordedRequest(nil), h.requests...)
}

func (h *fakeRemoteHost) find(method, path string) (recordedRequest, bool) {
	for _, req := range h.recorded() {
		if req.method == method && req.path == path {
			return req, true
		}
	}
	return recordedRequest{}, false
}

func writeTestJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeRemotesFile saves hosts the way the desktop does (0600) and points the
// CLI at it.
func writeRemotesFile(t *testing.T, entries ...remoteEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "remotes.json")
	raw, err := json.Marshal(map[string]any{"remotes": entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AO_REMOTES_FILE", path)
	return path
}

// localDaemon is this machine's loopback daemon: it records which paths the
// CLI sent locally (telemetry, identity, local project context).
type localDaemon struct {
	mu    sync.Mutex
	paths []string
	// linkStatus answers POST /api/v1/remote-worker-links; zero keeps the
	// route unknown (404) like a daemon that predates it.
	linkStatus      int
	linkPullBlocked string
	linkBodies      []string
	// linkCode is the error code answered with a non-201 linkStatus.
	linkCode string
	// checkCode, when set, makes POST remote-worker-links/check answer 409.
	checkCode string
	// checkAgent, when set, makes the check answer with a forced agent for a
	// request that names a host; checkBodies records every check request.
	checkAgent  string
	checkBodies []string
	// checkNeed makes a host-aware check without host readiness ask for these
	// harnesses first, as the daemon does when a profile selection applies.
	checkNeed []string
	// parentKind, when set, answers GET /api/v1/sessions/<parent> with a
	// live session of that kind.
	parentKind string
	// tailnet answers GET /api/v1/tailnet/hosts; nil keeps the route unknown.
	tailnet *tailnetHostsResponse
}

func (d *localDaemon) discover(res tailnetHostsResponse) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tailnet = &res
}

func (d *localDaemon) answerLinks(status int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.linkStatus = status
}

func (d *localDaemon) rejectLinksWith(status int, code string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.linkStatus, d.linkCode = status, code
}

func (d *localDaemon) rejectCheckWith(code string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.checkCode = code
}

func (d *localDaemon) links() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.linkBodies...)
}

func startLocalDaemon(t *testing.T, cfg testConfig, hostID string, projects map[string]projectDetails) *localDaemon {
	t.Helper()
	d := &localDaemon{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		d.paths = append(d.paths, r.Method+" "+r.URL.Path)
		d.mu.Unlock()
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/remote-worker-links/check" {
			checkBody, _ := io.ReadAll(r.Body)
			d.mu.Lock()
			code, forced, need := d.checkCode, d.checkAgent, d.checkNeed
			d.checkBodies = append(d.checkBodies, string(checkBody))
			d.mu.Unlock()
			if len(need) > 0 && strings.Contains(string(checkBody), `"agentCheck":true`) && !strings.Contains(string(checkBody), `"hostReadinessChecked":true`) {
				writeTestJSON(w, http.StatusOK, map[string]any{"ok": true, "readinessHarnesses": need})
				return
			}
			if forced != "" && strings.Contains(string(checkBody), `"agentCheck":true`) {
				writeTestJSON(w, http.StatusOK, map[string]any{"ok": true, "agent": map[string]string{
					"profileId": "p", "harness": forced, "model": "m", "effort": "e"}})
				return
			}
			if code != "" {
				writeTestJSON(w, http.StatusConflict, map[string]string{"error": "conflict", "code": code, "message": "worker limit reached"})
				return
			}
			writeTestJSON(w, http.StatusOK, map[string]bool{"ok": true})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/remote-worker-links" {
			body, _ := io.ReadAll(r.Body)
			d.mu.Lock()
			d.linkBodies = append(d.linkBodies, string(body))
			status, blocked, code := d.linkStatus, d.linkPullBlocked, d.linkCode
			if code == "" {
				code = "PARENT_SESSION_TERMINATED"
			}
			d.mu.Unlock()
			switch {
			case status == http.StatusCreated:
				writeTestJSON(w, status, map[string]string{"hostId": "host-build", "pullBlocked": blocked})
				return
			case status != 0:
				writeTestJSON(w, status, map[string]string{"error": "conflict", "code": code, "message": "link rejected"})
				return
			}
		}
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/sessions/") && d.parentKind != "":
			writeTestJSON(w, http.StatusOK, map[string]any{"session": map[string]any{
				"id": strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/"), "kind": d.parentKind, "isTerminated": false,
			}})
		case strings.HasPrefix(r.URL.Path, "/internal/"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/remote-worker-links/") &&
			strings.HasSuffix(r.URL.Path, "/release"):
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/api/v1/identity":
			writeTestJSON(w, http.StatusOK, map[string]any{"hostId": hostID, "apiVersion": 1})
		case r.URL.Path == "/api/v1/tailnet/hosts":
			d.mu.Lock()
			res := d.tailnet
			d.mu.Unlock()
			if res == nil {
				writeTestJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
				return
			}
			writeTestJSON(w, http.StatusOK, res)
		case strings.HasPrefix(r.URL.Path, "/api/v1/projects/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/projects/")
			project, ok := projects[id]
			if !ok {
				writeTestJSON(w, http.StatusNotFound, map[string]string{"message": "no project"})
				return
			}
			writeTestJSON(w, http.StatusOK, projectGetResult{Status: "ok", Project: project})
		default:
			writeTestJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
		}
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)
	return d
}

func (d *localDaemon) saw(entry string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, p := range d.paths {
		if p == entry {
			return true
		}
	}
	return false
}

func serveRemoteProjects(h *fakeRemoteHost, projects ...projectDetails) {
	summaries := make([]projectSummary, 0, len(projects))
	for _, project := range projects {
		summaries = append(summaries, projectSummary{ID: project.ID, Name: project.Name})
		h.handle(http.MethodGet, "/api/v1/projects/"+project.ID, func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(w, http.StatusOK, projectGetResult{Status: "ok", Project: project})
		})
	}
	h.handle(http.MethodGet, "/api/v1/projects", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, http.StatusOK, projectListResult{Projects: summaries})
	})
}

func TestRemotesFilePathResolution(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AO_REMOTES_FILE", "")
	desktop := filepath.Join(home, ".ao", "remotes.json")
	lab := filepath.Join(t.TempDir(), "lab-data")
	for _, tc := range []struct {
		name, dataDir, want string
	}{
		{name: "no data dir: desktop default", dataDir: "", want: desktop},
		// The packaged desktop gives agents AO_DATA_DIR=~/.ao/data but saves
		// hosts in ~/.ao/remotes.json; dev uses ~/.ao/dev/data the same way.
		{name: "packaged daemon data dir", dataDir: filepath.Join(home, ".ao", "data"), want: desktop},
		{name: "dev daemon data dir", dataDir: filepath.Join(home, ".ao", "dev", "data"), want: desktop},
		// An isolated AO_DATA_DIR is its own install: never fall back to the
		// user's real saved hosts, even when it has none yet.
		{name: "isolated lab data dir", dataDir: lab, want: filepath.Join(lab, "remotes.json")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AO_DATA_DIR", tc.dataDir)
			got, err := remotesFilePath()
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
	explicit := filepath.Join(t.TempDir(), "custom.json")
	t.Setenv("AO_REMOTES_FILE", explicit)
	if got, _ := remotesFilePath(); got != explicit {
		t.Fatalf("AO_REMOTES_FILE: got %s, want %s", got, explicit)
	}
}

func TestRemoteHTTPClientNeverUsesAProxy(t *testing.T) {
	proxyURL, _ := url.Parse("http://proxy.invalid:3128")
	for name, base := range map[string]*http.Client{
		"default transport":    {},
		"configured transport": {Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := &commandContext{deps: Deps{HTTPClient: base}}
			client := ctx.remoteHTTPClient(time.Second)
			transport, ok := client.Transport.(*http.Transport)
			if !ok {
				t.Fatalf("transport = %T, want *http.Transport", client.Transport)
			}
			if transport.Proxy != nil {
				t.Fatal("remote client would send the bearer through an environment or configured proxy")
			}
			if base.Transport != nil && base.Transport.(*http.Transport).Proxy == nil {
				t.Fatal("remoteHTTPClient mutated the shared loopback transport")
			}
		})
	}
}

func TestReadRemoteEntriesRefusesFileReadableByOthers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not enforced on Windows")
	}
	path := filepath.Join(t.TempDir(), "remotes.json")
	if err := os.WriteFile(path, []byte(`{"remotes":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readRemoteEntries(path); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("err = %v, want refusal with chmod guidance", err)
	}
	if entries, err := readRemoteEntries(filepath.Join(t.TempDir(), "missing.json")); err != nil || len(entries) != 0 {
		t.Fatalf("missing file: entries=%v err=%v, want empty", entries, err)
	}
}

func TestSelectRemoteEntry(t *testing.T) {
	entries := []remoteEntry{
		{Label: "Build Box", URL: "http://10.0.0.5:3101", HostID: "host-a"},
		{Label: "gpu", URL: "https://gpu.example.ts.net", HostID: "host-b"},
		{Label: "twin", URL: "http://10.0.0.7:3101", HostID: "host-c"},
		{Label: "TWIN", URL: "http://10.0.0.8:3101", HostID: "host-d"},
	}
	for _, tc := range []struct {
		selector, wantHost, wantErr string
	}{
		{selector: "host-b", wantHost: "host-b"},
		{selector: "build box", wantHost: "host-a"},
		{selector: "10.0.0.5:3101", wantHost: "host-a"},
		{selector: "https://gpu.example.ts.net/", wantHost: "host-b"},
		{selector: "twin", wantErr: "matches 2 saved hosts"},
		{selector: "nope", wantErr: "matches no remote host"},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			got, err := selectRemoteEntry(entries, tc.selector)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || ExitCode(err) != 2 {
					t.Fatalf("err = %v, want usage error containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || got.HostID != tc.wantHost {
				t.Fatalf("got %+v err %v, want %s", got, err, tc.wantHost)
			}
		})
	}
	if _, err := selectRemoteEntry(nil, "x"); err == nil || !strings.Contains(err.Error(), "no remote hosts") {
		t.Fatalf("empty list err = %v", err)
	}
}

func TestRemoteSpawnTargetsPairedHostAndMatchesProjectByRepository(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "ao-7")
	t.Setenv("AO_PROJECT_ID", "local-widgets")
	local := startLocalDaemon(t, cfg, "host-local", map[string]projectDetails{
		"local-widgets": {ID: "local-widgets", Repo: "git@github.com:Acme/Widgets.git"},
	})
	local.answerLinks(http.StatusCreated)
	remote := newFakeRemoteHost(t, "host-build", "s3cret")
	serveRemoteProjects(remote,
		projectDetails{ID: "other", Repo: "https://github.com/acme/other"},
		projectDetails{ID: "widgets", Repo: "https://github.com/acme/widgets", Agent: "claude-code"},
	)
	remote.handle(http.MethodPost, "/api/v1/sessions", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, http.StatusCreated, map[string]any{"session": map[string]string{"id": "widgets-3", "status": "spawning", "displayName": "auth refactor"}})
	})
	writeRemotesFile(t, remoteEntry{Label: "build-box", URL: remote.srv.URL, Password: "s3cret", HostID: "host-build"})

	out, errOut, err := executeCLI(t, aliveDeps(), "spawn", "--host", "build-box", "--name", "auth refactor", "--prompt", "do it", "--agent", "claude-code", "--skip-agent-check")
	if err != nil {
		t.Fatalf("spawn: %v\nstderr=%s", err, errOut)
	}
	if !strings.Contains(out, `spawned session widgets-3 "auth refactor" (spawning) on host "build-box"`) {
		t.Fatalf("stdout = %q", out)
	}
	spawn, ok := remote.find(http.MethodPost, "/api/v1/sessions")
	if !ok {
		t.Fatal("spawn never reached the remote host")
	}
	if spawn.auth != "Bearer s3cret" || spawn.expectedHost != "host-build" {
		t.Fatalf("spawn headers auth=%q expected=%q", spawn.auth, spawn.expectedHost)
	}
	var req map[string]any
	if err := json.Unmarshal([]byte(spawn.body), &req); err != nil {
		t.Fatal(err)
	}
	if req["projectId"] != "widgets" {
		t.Fatalf("projectId = %v, want widgets (matched by repository, not the local ID)", req["projectId"])
	}
	if _, present := req["parentSessionId"]; present {
		t.Fatalf("parentSessionId forwarded to a remote daemon: %v", req["parentSessionId"])
	}
	for _, r := range remote.recorded() {
		if strings.HasPrefix(r.path, "/internal/") {
			t.Fatalf("loopback-only route %s reached the remote host", r.path)
		}
		if r.path == "/api/v1/identity" && r.auth != "" {
			t.Fatal("password sent with the identity probe")
		}
	}
	if !local.saw("POST /internal/telemetry/cli-invoked") {
		t.Fatal("CLI telemetry should stay on the local daemon")
	}
}

func TestRemoteSpawnWithoutLocalContextRequiresProject(t *testing.T) {
	setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "")
	t.Chdir(t.TempDir())
	remote := newFakeRemoteHost(t, "host-build", "pw")
	serveRemoteProjects(remote, projectDetails{ID: "widgets", Repo: "https://github.com/acme/widgets"})
	writeRemotesFile(t, remoteEntry{Label: "build-box", URL: remote.srv.URL, Password: "pw", HostID: "host-build"})

	_, _, err := executeCLI(t, aliveDeps(), "spawn", "--host", "build-box", "--name", "x", "--agent", "codex", "--skip-agent-check")
	if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "--project is required with --host") || !strings.Contains(err.Error(), "widgets") {
		t.Fatalf("err = %v, want usage error listing remote projects", err)
	}
	if _, ok := remote.find(http.MethodPost, "/api/v1/sessions"); ok {
		t.Fatal("spawned without a resolved remote project")
	}
}

func TestRemoteSpawnNumericClaimPRNeverRunsLocalGH(t *testing.T) {
	setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "")
	remote := newFakeRemoteHost(t, "host-build", "pw")
	serveRemoteProjects(remote, projectDetails{ID: "widgets", Path: "/srv/widgets", Agent: "codex"})
	writeRemotesFile(t, remoteEntry{Label: "build-box", URL: remote.srv.URL, Password: "pw", HostID: "host-build"})
	ranGH := false
	deps := aliveDeps()
	deps.CommandOutputInDir = func(context.Context, string, string, ...string) ([]byte, error) {
		ranGH = true
		return []byte("https://github.com/acme/widgets\n"), nil
	}
	_, _, err := executeCLI(t, deps, "spawn", "--host", "build-box", "--project", "widgets", "--name", "x", "--agent", "codex", "--claim-pr", "12", "--skip-agent-check")
	if err == nil || !strings.Contains(err.Error(), "pass the full PR URL") {
		t.Fatalf("err = %v, want full-URL guidance", err)
	}
	if ranGH {
		t.Fatal("ran gh in a local directory for a remote project path")
	}
}

func TestRemoteIdentityMismatchNeverSendsPassword(t *testing.T) {
	setConfigEnv(t)
	remote := newFakeRemoteHost(t, "someone-else", "pw")
	writeRemotesFile(t, remoteEntry{Label: "build-box", URL: remote.srv.URL, Password: "pw", HostID: "host-build"})

	_, _, err := executeCLI(t, aliveDeps(), "project", "ls", "--host", "build-box")
	if err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("err = %v, want identity refusal", err)
	}
	for _, r := range remote.recorded() {
		if r.auth != "" || r.path != "/api/v1/identity" {
			t.Fatalf("request after failed identity check: %+v", r)
		}
	}
}

func TestRemoteHostWithoutRecordedIdentityMustBeRepaired(t *testing.T) {
	setConfigEnv(t)
	remote := newFakeRemoteHost(t, "host-build", "pw")
	writeRemotesFile(t, remoteEntry{Label: "legacy", URL: remote.srv.URL, Password: "pw"})

	_, _, err := executeCLI(t, aliveDeps(), "project", "ls", "--host", "legacy")
	if err == nil || !strings.Contains(err.Error(), "paired again") {
		t.Fatalf("err = %v, want re-pair guidance", err)
	}
	if n := len(remote.recorded()); n != 0 {
		t.Fatalf("contacted an unpaired host %d times", n)
	}
}

func TestRemoteTargetRefusesThisMachine(t *testing.T) {
	cfg := setConfigEnv(t)
	startLocalDaemon(t, cfg, "host-same", nil)
	remote := newFakeRemoteHost(t, "host-same", "pw")
	writeRemotesFile(t, remoteEntry{Label: "me", URL: remote.srv.URL, Password: "pw", HostID: "host-same"})

	_, _, err := executeCLI(t, aliveDeps(), "project", "ls", "--host", "me")
	if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "is this machine") {
		t.Fatalf("err = %v, want usage error for self-targeting", err)
	}
	if _, ok := remote.find(http.MethodGet, "/api/v1/projects"); ok {
		t.Fatal("sent a request to this machine through its remote listener")
	}
}

func TestRemoteRedirectIsNotFollowedWithCredential(t *testing.T) {
	setConfigEnv(t)
	var leaked atomic.Bool
	elsewhere := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leaked.Store(true)
		}
	}))
	t.Cleanup(elsewhere.Close)
	remote := newFakeRemoteHost(t, "host-build", "pw")
	remote.handle(http.MethodGet, "/api/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/api/v1/projects", http.StatusTemporaryRedirect)
	})
	writeRemotesFile(t, remoteEntry{Label: "build-box", URL: remote.srv.URL, Password: "pw", HostID: "host-build"})

	_, _, err := executeCLI(t, aliveDeps(), "project", "ls", "--host", "build-box")
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("err = %v, want redirect refusal", err)
	}
	if leaked.Load() {
		t.Fatal("bearer credential followed a redirect to another origin")
	}
}

func TestRemoteUnauthorizedExplainsSavedPassword(t *testing.T) {
	setConfigEnv(t)
	remote := newFakeRemoteHost(t, "host-build", "new-password")
	writeRemotesFile(t, remoteEntry{Label: "build-box", URL: remote.srv.URL, Password: "old-password", HostID: "host-build"})

	_, _, err := executeCLI(t, aliveDeps(), "session", "ls", "--host", "build-box")
	if err == nil || !strings.Contains(err.Error(), "rejected the saved password") || !strings.Contains(err.Error(), "BAD_PASSWORD") {
		t.Fatalf("err = %v, want saved-password guidance with the daemon envelope", err)
	}
}

func TestRemoteSendKeepsAttributionInTextOnly(t *testing.T) {
	setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "ao-7")
	remote := newFakeRemoteHost(t, "host-build", "pw")
	remote.handle(http.MethodPost, "/api/v1/sessions/widgets-3/send", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	writeRemotesFile(t, remoteEntry{Label: "build-box", URL: remote.srv.URL, Password: "pw", HostID: "host-build"})

	if _, errOut, err := executeCLI(t, aliveDeps(), "send", "--host", "build-box", "--session", "widgets-3", "--message", "CI failed, fix the test"); err != nil {
		t.Fatalf("send: %v\nstderr=%s", err, errOut)
	}
	sent, ok := remote.find(http.MethodPost, "/api/v1/sessions/widgets-3/send")
	if !ok {
		t.Fatal("send never reached the remote host")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(sent.body), &body); err != nil {
		t.Fatal(err)
	}
	if _, present := body["senderSessionId"]; present {
		t.Fatalf("local senderSessionId forwarded to a remote daemon: %v", body["senderSessionId"])
	}
	if body["userAuthored"] == true {
		t.Fatal("an agent-sent message was marked user-authored")
	}
	host, _ := os.Hostname()
	if want := fmt.Sprintf("[from ao-7 on %s] CI failed, fix the test", host); body["message"] != want {
		t.Fatalf("message = %q, want %q", body["message"], want)
	}
}

func TestHostFlagIsRejectedByLocalOnlyCommands(t *testing.T) {
	setConfigEnv(t)
	for _, args := range [][]string{
		{"status", "--host", "build-box"},
		{"stop", "--host", "build-box"},
		{"project", "add", "--host", "build-box"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, _, err := executeCLI(t, Deps{}, args...)
			if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "unknown flag: --host") {
				t.Fatalf("err = %v, want unknown-flag usage error", err)
			}
		})
	}
}

func TestHostListReportsReachabilityWithoutLeakingPasswords(t *testing.T) {
	setConfigEnv(t)
	online := newFakeRemoteHost(t, "host-online", "pw-online")
	stale := newFakeRemoteHost(t, "host-stale", "rotated")
	moved := newFakeRemoteHost(t, "host-impostor", "pw-moved")
	for _, h := range []*fakeRemoteHost{online, stale, moved} {
		h.handle(http.MethodGet, "/healthz", func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})
	}
	writeRemotesFile(t,
		remoteEntry{Label: "online", URL: online.srv.URL, Password: "pw-online", HostID: "host-online"},
		remoteEntry{Label: "stale", URL: stale.srv.URL, Password: "pw-stale", HostID: "host-stale"},
		remoteEntry{Label: "moved", URL: moved.srv.URL, Password: "pw-moved", HostID: "host-moved"},
		remoteEntry{Label: "legacy", URL: "http://127.0.0.1:1", Password: "pw-legacy"},
	)

	out, errOut, err := executeCLI(t, aliveDeps(), "host", "ls", "--json")
	if err != nil {
		t.Fatalf("host ls: %v\nstderr=%s", err, errOut)
	}
	if strings.Contains(out, "pw-") {
		t.Fatalf("host ls printed a saved password:\n%s", out)
	}
	var parsed struct {
		Hosts []hostListItem `json:"hosts"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, item := range parsed.Hosts {
		got[item.Label] = item.Status
	}
	want := map[string]string{"online": hostStatusOnline, "stale": hostStatusUnauthorized, "moved": hostStatusIdentityChange, "legacy": hostStatusNeedsPairing}
	for label, status := range want {
		if got[label] != status {
			t.Errorf("%s status = %q, want %q (all: %v)", label, got[label], status, got)
		}
	}
	for _, r := range moved.recorded() {
		if r.auth != "" {
			t.Fatal("password sent to a host whose identity did not match")
		}
	}
}

func TestRemoteBaseURLKeepsHTTPS(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"https://worker.up.railway.app", "https://worker.up.railway.app"},
		{"https://worker.up.railway.app/", "https://worker.up.railway.app"},
		{"HTTPS://Worker.Up.Railway.App", "https://worker.up.railway.app"},
		{"https://worker.up.railway.app:8443", "https://worker.up.railway.app:8443"},
		{"worker.railway.internal:3011", "http://worker.railway.internal:3011"},
	} {
		if got, err := remoteBaseURL(tc.raw); err != nil || got != tc.want {
			t.Errorf("remoteBaseURL(%q) = %q, %v; want %q", tc.raw, got, err, tc.want)
		}
	}
	for _, raw := range []string{"ftp://worker.up.railway.app", "https://user:pw@worker.up.railway.app", "https://"} {
		if _, err := remoteBaseURL(raw); err == nil {
			t.Errorf("remoteBaseURL(%q) accepted an unsafe address", raw)
		}
	}
}

// A worker behind a TLS-terminating proxy is saved with an https URL. Both
// `ao host ls` and --host must probe identity and then talk over TLS.
func TestRemoteHTTPSHost(t *testing.T) {
	setConfigEnv(t)
	remote := newFakeRemoteHostTLS(t, "host-worker", "pw-worker")
	remote.handle(http.MethodGet, "/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	serveRemoteProjects(remote, projectDetails{ID: "widgets", Name: "Widgets"})
	if !strings.HasPrefix(remote.srv.URL, "https://") {
		t.Fatalf("fake host URL %q is not https", remote.srv.URL)
	}
	writeRemotesFile(t, remoteEntry{Label: "worker", URL: remote.srv.URL + "/", Password: "pw-worker", HostID: "host-worker"})
	deps := aliveDeps()
	deps.HTTPClient = remote.srv.Client() // trusts the test certificate

	out, errOut, err := executeCLI(t, deps, "host", "ls", "--json")
	if err != nil {
		t.Fatalf("host ls: %v\nstderr=%s", err, errOut)
	}
	var listed struct {
		Hosts []hostListItem `json:"hosts"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Hosts) != 1 || listed.Hosts[0].HostID != "host-worker" || listed.Hosts[0].Status != hostStatusOnline {
		t.Fatalf("host ls = %+v, want worker online with its host ID", listed.Hosts)
	}

	out, errOut, err = executeCLI(t, deps, "project", "ls", "--host", "worker")
	if err != nil {
		t.Fatalf("project ls --host worker: %v\nstderr=%s", err, errOut)
	}
	if !strings.Contains(out, "widgets") {
		t.Fatalf("project ls --host worker output %q does not list the remote project", out)
	}
	got, ok := remote.find(http.MethodGet, "/api/v1/projects")
	if !ok || got.auth != "Bearer pw-worker" || got.expectedHost != "host-worker" {
		t.Fatalf("project list request = %+v (found %v), want bearer and expected host ID", got, ok)
	}
	for _, r := range remote.recorded() {
		if !r.tls {
			t.Fatalf("request %s %s left TLS", r.method, r.path)
		}
		if r.path == "/api/v1/identity" && r.auth != "" {
			t.Fatal("identity probe carried a credential")
		}
	}
}

func TestHostListWithoutSavedHosts(t *testing.T) {
	setConfigEnv(t)
	t.Setenv("AO_REMOTES_FILE", filepath.Join(t.TempDir(), "missing.json"))
	out, _, err := executeCLI(t, Deps{}, "host", "ls")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No remote hosts") {
		t.Fatalf("stdout = %q", out)
	}
}

// Tailnet hosts are listed and usable with --host without a password; a saved
// entry with the same host ID moves to the Tailscale address, self never shows.
func TestTailnetDiscoveredHostsNeedNoPassword(t *testing.T) {
	cfg := setConfigEnv(t)
	z600 := newFakeRemoteHost(t, "host-z600", "")
	z600.handle(http.MethodGet, "/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	serveRemoteProjects(z600, projectDetails{ID: "widgets", Name: "Widgets"})
	writeRemotesFile(t,
		remoteEntry{Label: "Z600 (LAN)", URL: "http://192.0.2.10:3011", Password: "pw-old", HostID: "host-z600"},
		remoteEntry{Label: "Linux", URL: "http://192.0.2.11:3011", Password: "pw-self", HostID: "host-old-self"},
		remoteEntry{Label: "tunnel-box", URL: "http://127.0.0.1:1", Password: "pw-tunnel", HostID: "host-tunnel"},
	)
	local := startLocalDaemon(t, cfg, "host-local", nil)
	local.discover(tailnetHostsResponse{
		Available: true, LocalHostIDs: []string{"host-local", "host-old-self"},
		Hosts: []tailnetHost{
			{HostID: "host-z600", Label: "hpz600", Name: "hpz600", Address: "127.0.0.1", URL: z600.srv.URL, Online: true},
			{HostID: "host-air", Label: "petars-macbook-air", URL: "http://100.126.82.17:3011", Online: false},
			{HostID: "host-hidden", Label: "hidden", URL: "http://100.1.1.1:3011", Online: true, Hidden: true},
		},
	})

	out, errOut, err := executeCLI(t, aliveDeps(), "host", "ls", "--json")
	if err != nil {
		t.Fatalf("host ls: %v\nstderr=%s", err, errOut)
	}
	var listed hostListOutput
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatal(err)
	}
	got := map[string]hostListItem{}
	for _, item := range listed.Hosts {
		got[item.HostID] = item
	}
	if len(got) != 3 || !listed.Tailscale.Available {
		t.Fatalf("hosts = %+v tailscale = %+v", listed.Hosts, listed.Tailscale)
	}
	if item := got["host-z600"]; item.Status != hostStatusOnline || item.Source != hostSourceTailscale || item.URL != z600.srv.URL || item.Label != "hpz600" {
		t.Fatalf("z600 = %+v", item)
	}
	if item := got["host-air"]; item.Status != hostStatusOffline || item.Source != hostSourceTailscale {
		t.Fatalf("air = %+v", item)
	}
	if item := got["host-tunnel"]; item.Source != hostSourceSaved {
		t.Fatalf("tunnel-box = %+v", item)
	}
	if strings.Contains(out, "pw-") {
		t.Fatalf("host ls printed a saved password:\n%s", out)
	}

	out, errOut, err = executeCLI(t, aliveDeps(), "project", "ls", "--host", "hpz600")
	if err != nil {
		t.Fatalf("project ls --host hpz600: %v\nstderr=%s", err, errOut)
	}
	if !strings.Contains(out, "widgets") {
		t.Fatalf("project ls output %q does not list the remote project", out)
	}
	req, ok := z600.find(http.MethodGet, "/api/v1/projects")
	if !ok || req.auth != "" || req.expectedHost != "host-z600" {
		t.Fatalf("project list request = %+v (found %v), want no bearer and the expected host ID", req, ok)
	}

	_, _, err = executeCLI(t, aliveDeps(), "project", "ls", "--host", "petars-macbook-air")
	if err == nil || !strings.Contains(err.Error(), "offline on the tailnet") {
		t.Fatalf("offline tailnet host err = %v", err)
	}
	_, _, err = executeCLI(t, aliveDeps(), "project", "ls", "--host", "Linux")
	if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "is this machine") {
		t.Fatalf("own old host ID err = %v", err)
	}
}

// A Tailscale host that refuses this machine's identity says so, and the CLI
// never falls back to a password it does not have.
func TestTailnetHostRejectionIsExplained(t *testing.T) {
	cfg := setConfigEnv(t)
	host := newFakeRemoteHost(t, "host-other", "")
	host.handle(http.MethodGet, "/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, http.StatusForbidden, map[string]string{"code": "TAILSCALE_UNAVAILABLE", "message": "Tailscale is unavailable on this host"})
	})
	host.handle(http.MethodGet, "/api/v1/projects", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, http.StatusForbidden, map[string]string{"code": "TAILNET_FORBIDDEN", "message": "other user"})
	})
	t.Setenv("AO_REMOTES_FILE", filepath.Join(t.TempDir(), "missing.json"))
	local := startLocalDaemon(t, cfg, "host-local", nil)
	local.discover(tailnetHostsResponse{Available: true, Hosts: []tailnetHost{{HostID: "host-other", Label: "other", URL: host.srv.URL, Online: true}}})

	out, _, err := executeCLI(t, aliveDeps(), "host", "ls")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, hostStatusUnauthorized) || !strings.Contains(out, "Tailscale unavailable on that host") {
		t.Fatalf("host ls = %q", out)
	}
	_, _, err = executeCLI(t, aliveDeps(), "project", "ls", "--host", "other")
	if err == nil || !strings.Contains(err.Error(), "did not admit this machine's Tailscale identity") || !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("err = %v", err)
	}
}

// Without tailscaled the list says so instead of hanging or hiding saved hosts.
func TestHostListReportsTailscaleUnavailable(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_REMOTES_FILE", filepath.Join(t.TempDir(), "missing.json"))
	local := startLocalDaemon(t, cfg, "host-local", nil)
	local.discover(tailnetHostsResponse{Available: false, Error: "tailscale is unavailable: exec: not found"})
	out, _, err := executeCLI(t, aliveDeps(), "host", "ls")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Tailscale unavailable: tailscale is unavailable") || !strings.Contains(out, "No remote hosts") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestRemoteTransportFailureIsDaemonUnavailable(t *testing.T) {
	setConfigEnv(t)
	remote := newFakeRemoteHost(t, "host-build", "pw")
	writeRemotesFile(t, remoteEntry{Label: "build-box", URL: remote.srv.URL, Password: "pw", HostID: "host-build"})
	remote.srv.Close()

	_, _, err := executeCLI(t, aliveDeps(), "session", "ls", "--host", "build-box")
	if err == nil || !strings.Contains(err.Error(), `remote host "build-box"`) {
		t.Fatalf("err = %v, want unreachable remote host", err)
	}
	if errors.Is(err, errDaemonNotRunning) {
		t.Fatal("a remote outage must not be reported as the local daemon not running")
	}
}

func TestRemoteIdentityProbeRefusesRedirect(t *testing.T) {
	setConfigEnv(t)
	var reached atomic.Bool
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached.Store(true)
		writeTestJSON(w, http.StatusOK, map[string]any{"hostId": "host-build", "apiVersion": 1})
	}))
	t.Cleanup(elsewhere.Close)
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(redirector.Close)
	writeRemotesFile(t, remoteEntry{Label: "build-box", URL: redirector.URL, Password: "pw", HostID: "host-build"})

	_, _, err := executeCLI(t, aliveDeps(), "project", "ls", "--host", "build-box")
	if err == nil || !strings.Contains(err.Error(), "identity probe returned HTTP 302") {
		t.Fatalf("err = %v, want refused identity redirect", err)
	}
	if reached.Load() {
		t.Fatal("identity probe followed a redirect to another origin")
	}
}

func TestRemoteSpawnProjectMatchingRequiresExactlyOneRepository(t *testing.T) {
	for _, tc := range []struct {
		name     string
		projects []projectDetails
		wantErr  string
	}{
		{name: "no match", projects: []projectDetails{{ID: "other", Repo: "https://github.com/acme/other"}}, wantErr: "no project on the remote host uses repository github.com/acme/widgets"},
		{name: "several matches", projects: []projectDetails{
			{ID: "widgets-a", Repo: "https://github.com/acme/widgets"},
			{ID: "widgets-b", Repo: "git@github.com:acme/widgets.git"},
		}, wantErr: "several remote projects use repository github.com/acme/widgets (widgets-a, widgets-b)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := setConfigEnv(t)
			t.Setenv("AO_SESSION_ID", "")
			t.Setenv("AO_PROJECT_ID", "local-widgets")
			startLocalDaemon(t, cfg, "host-local", map[string]projectDetails{
				"local-widgets": {ID: "local-widgets", Repo: "https://github.com/acme/widgets"},
			})
			remote := newFakeRemoteHost(t, "host-build", "pw")
			serveRemoteProjects(remote, tc.projects...)
			writeRemotesFile(t, remoteEntry{Label: "build-box", URL: remote.srv.URL, Password: "pw", HostID: "host-build"})

			_, _, err := executeCLI(t, aliveDeps(), "spawn", "--host", "build-box", "--name", "x", "--agent", "codex", "--skip-agent-check")
			if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want usage error containing %q", err, tc.wantErr)
			}
			if _, ok := remote.find(http.MethodPost, "/api/v1/sessions"); ok {
				t.Fatal("spawned without exactly one matching remote project")
			}
		})
	}
}

func TestRemoteSteerRecoveryHintKeepsHost(t *testing.T) {
	setConfigEnv(t)
	t.Setenv("AO_SESSION_ID", "ao-7")
	remote := newFakeRemoteHost(t, "host-build", "pw")
	remote.handle(http.MethodPost, "/api/v1/sessions/widgets-3/conversation/steer-or-send", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, http.StatusConflict, map[string]string{"message": "the provider may have received this guidance", "code": "CHAT_STEER_UNCERTAIN"})
	})
	writeRemotesFile(t, remoteEntry{Label: "Build Box", URL: remote.srv.URL, Password: "pw", HostID: "host-build"})

	_, _, err := executeCLI(t, aliveDeps(), "send", "--host", "Build Box", "--session", "widgets-3", "--steer", "--client-message-id", "h-1", "--message", "stop")
	if err == nil || !strings.Contains(err.Error(), "ao send --host 'Build Box' --session widgets-3 --steer --recover-only --client-message-id h-1") {
		t.Fatalf("err = %v, want a recovery command that targets the same host", err)
	}
	steer, ok := remote.find(http.MethodPost, "/api/v1/sessions/widgets-3/conversation/steer-or-send")
	if !ok || strings.Contains(steer.body, "senderSessionId") {
		t.Fatalf("steer request = %+v, want it on the remote host without a local senderSessionId", steer)
	}
}

func TestHostFlagRoutesEveryOptedInCommandToTheRemoteHost(t *testing.T) {
	for _, tc := range []struct {
		args         []string
		method, path string
	}{
		{args: []string{"session", "get", "widgets-3"}, method: http.MethodGet, path: "/api/v1/sessions/widgets-3"},
		{args: []string{"session", "top"}, method: http.MethodGet, path: "/api/v1/sessions"},
		{args: []string{"session", "kill", "widgets-3"}, method: http.MethodPost, path: "/api/v1/sessions/widgets-3/kill"},
		{args: []string{"session", "rename", "widgets-3", "new name"}, method: http.MethodPatch, path: "/api/v1/sessions/widgets-3"},
		{args: []string{"session", "exit-agent", "widgets-3"}, method: http.MethodPost, path: "/api/v1/sessions/widgets-3/exit-agent"},
		{args: []string{"session", "resume-agent", "widgets-3"}, method: http.MethodPost, path: "/api/v1/sessions/widgets-3/resume-agent"},
		{args: []string{"project", "get", "widgets"}, method: http.MethodGet, path: "/api/v1/projects/widgets"},
		{args: []string{"project", "set-config", "widgets", "--github-account", "work-account"}, method: http.MethodPut, path: "/api/v1/projects/widgets/config"},
		{args: []string{"orchestrator", "ls"}, method: http.MethodGet, path: "/api/v1/orchestrators"},
		{args: []string{"agent", "ls"}, method: http.MethodPost, path: "/api/v1/agents/readiness/ensure"},
		{args: []string{"review", "ls", "widgets-3"}, method: http.MethodGet, path: "/api/v1/sessions/widgets-3/reviews"},
		{args: []string{"review", "trigger", "widgets-3"}, method: http.MethodPost, path: "/api/v1/sessions/widgets-3/reviews/trigger"},
		{args: []string{"review", "cancel", "widgets-3"}, method: http.MethodPost, path: "/api/v1/sessions/widgets-3/reviews/cancel"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			cfg := setConfigEnv(t)
			t.Setenv("AO_REVIEW_SESSION_ID", "")
			local := startLocalDaemon(t, cfg, "host-local", nil)
			remote := newFakeRemoteHost(t, "host-build", "pw")
			writeRemotesFile(t, remoteEntry{Label: "build-box", URL: remote.srv.URL, Password: "pw", HostID: "host-build"})

			// The fake host answers 404 for these routes; only where the
			// request went matters here.
			_, _, _ = executeCLI(t, aliveDeps(), append(tc.args, "--host", "build-box")...)
			got, ok := remote.find(tc.method, tc.path)
			if !ok {
				t.Fatalf("%s %s never reached the remote host; saw %+v", tc.method, tc.path, remote.recorded())
			}
			if got.auth != "Bearer pw" || got.expectedHost != "host-build" {
				t.Fatalf("headers auth=%q expected=%q", got.auth, got.expectedHost)
			}
			if local.saw(tc.method + " " + tc.path) {
				t.Fatalf("%s %s also went to the local daemon", tc.method, tc.path)
			}
		})
	}
}

func TestHostFlagHelpNamesItsArgument(t *testing.T) {
	// pflag treats back-quoted text in a usage string as the argument name, so
	// the help must not quote a command with back-quotes.
	out, _, err := executeCLI(t, Deps{}, "spawn", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "--host string") {
		t.Fatalf("spawn --help does not show `--host string`:\n%s", out)
	}
}
