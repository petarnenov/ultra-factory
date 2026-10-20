package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sessionTopServer(t *testing.T, memoryStatus int, memoryBody string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sessions":
			_, _ = io.WriteString(w, `{"sessions":[`+
				sessionJSON("demo-1", "demo", "worker", "working", false)+`,`+
				sessionJSON("demo-2", "demo", "worker", "idle", false)+`,`+
				sessionJSON("demo-3", "demo", "worker", "idle", false)+`]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/usage/sessions/memory":
			w.WriteHeader(memoryStatus)
			_, _ = io.WriteString(w, memoryBody)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

const topMemoryBody = `{"sessions":[
	{"sessionId":"demo-1","rssBytes":641728512,"processCount":5,"sampledAt":"2026-09-18T00:00:00Z","processes":[]},
	{"sessionId":"demo-2","rssBytes":2254857830,"processCount":9,"sampledAt":"2026-09-18T00:00:00Z","processes":[]}]}`

func TestSessionTop_SortsByMemoryAndDashesUnsampled(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := sessionTopServer(t, http.StatusOK, topMemoryBody)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "session", "top")
	if err != nil {
		t.Fatalf("session top failed: %v\nstderr=%s", err, errOut)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected header, 3 rows and total, got:\n%s", out)
	}
	if !strings.HasPrefix(lines[1], "demo-2") || !strings.Contains(lines[1], "2.1 GB") || !strings.Contains(lines[1], "9") {
		t.Fatalf("largest session should lead:\n%s", out)
	}
	if !strings.HasPrefix(lines[2], "demo-1") || !strings.Contains(lines[2], "612.0 MB") {
		t.Fatalf("second row wrong:\n%s", out)
	}
	if !strings.HasPrefix(lines[3], "demo-3") || !strings.Contains(lines[3], "  -  ") {
		t.Fatalf("unsampled session should show dashes, not zero:\n%s", out)
	}
	if !strings.HasPrefix(lines[4], "TOTAL") || !strings.Contains(lines[4], "2.7 GB") {
		t.Fatalf("total row wrong:\n%s", out)
	}
}

// TestSessionTop_JSONDoesNotFabricateZeroForUnmeasured guards the JSON
// contract against synthesizing a measurement: demo-3 has no entry in the
// memory response (unsampled), so its JSON fields must be null, exactly like
// the text table shows "-" instead of "0 B". A bare zero there would be
// indistinguishable from a genuinely-empty process tree to any automation
// reading --json.
func TestSessionTop_JSONDoesNotFabricateZeroForUnmeasured(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := sessionTopServer(t, http.StatusOK, topMemoryBody)
	writeRunFileFor(t, cfg, srv)

	out, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "session", "top", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Data []struct {
			ID           string  `json:"id"`
			RSSBytes     *uint64 `json:"rssBytes"`
			ProcessCount *int    `json:"processCount"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out)
	}
	var found bool
	for _, e := range parsed.Data {
		if e.ID != "demo-3" {
			continue
		}
		found = true
		if e.RSSBytes != nil || e.ProcessCount != nil {
			t.Fatalf("unmeasured session must serialize as null, not a fabricated zero: rssBytes=%v processCount=%v", e.RSSBytes, e.ProcessCount)
		}
	}
	if !found {
		t.Fatal("demo-3 missing from json output")
	}
}

func TestSessionTop_JSONIncludesTotal(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := sessionTopServer(t, http.StatusOK, topMemoryBody)
	writeRunFileFor(t, cfg, srv)

	out, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "session", "top", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"totalRssBytes": 2896586342`) {
		t.Fatalf("json missing total:\n%s", out)
	}
}

func TestSessionTop_SurfacesDaemonError(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := sessionTopServer(t, http.StatusNotImplemented, `{"error":"not_implemented","code":"MEMORY_UNSUPPORTED","message":"procmem: process memory is not supported on windows"}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "session", "top")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errOut+err.Error(), "MEMORY_UNSUPPORTED") && !strings.Contains(errOut+err.Error(), "not supported") {
		t.Fatalf("error should carry daemon envelope: %v\n%s", err, errOut)
	}
}

// TestSessionTopRemoteHostReadsTheRemoteDaemon: --host routes both reads to
// the paired host and keeps the local table and JSON shape.
func TestSessionTopRemoteHostReadsTheRemoteDaemon(t *testing.T) {
	cfg := setConfigEnv(t)
	local := startLocalDaemon(t, cfg, "host-local", nil)
	remote := newFakeRemoteHost(t, "host-build", "pw")
	remote.handle(http.MethodGet, "/api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("active"); got != "true" {
			t.Errorf("remote sessions active = %q, want true", got)
		}
		if got := r.URL.Query().Get("project"); got != "widgets" {
			t.Errorf("remote sessions project = %q, want widgets", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"sessions":[`+
			sessionJSON("demo-1", "widgets", "worker", "working", false)+`,`+
			sessionJSON("demo-2", "widgets", "worker", "idle", false)+`,`+
			sessionJSON("demo-3", "widgets", "worker", "idle", false)+`]}`)
	})
	remote.handle(http.MethodGet, "/api/v1/usage/sessions/memory", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("projectId"); got != "widgets" {
			t.Errorf("remote memory projectId = %q, want widgets", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, topMemoryBody)
	})
	writeRemotesFile(t, remoteEntry{Label: "build-box", URL: remote.srv.URL, Password: "pw", HostID: "host-build"})

	out, errOut, err := executeCLI(t, aliveDeps(), "session", "top", "--host", "build-box", "--project", "widgets")
	if err != nil {
		t.Fatalf("session top --host failed: %v\nstderr=%s", err, errOut)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected the local table shape (header, 3 rows, total), got:\n%s", out)
	}
	if !strings.HasPrefix(lines[1], "demo-2") || !strings.Contains(lines[1], "2.1 GB") {
		t.Fatalf("largest remote session should lead:\n%s", out)
	}
	if !strings.HasPrefix(lines[4], "TOTAL") || !strings.Contains(lines[4], "2.7 GB") {
		t.Fatalf("total row wrong:\n%s", out)
	}
	for _, path := range []string{"/api/v1/sessions", "/api/v1/usage/sessions/memory"} {
		req, ok := remote.find(http.MethodGet, path)
		if !ok {
			t.Fatalf("%s never reached the remote host; saw %+v", path, remote.recorded())
		}
		if req.auth != "Bearer pw" || req.expectedHost != "host-build" {
			t.Fatalf("%s headers auth=%q expected=%q", path, req.auth, req.expectedHost)
		}
		if local.saw("GET " + path) {
			t.Fatalf("%s also went to the local daemon", path)
		}
	}
}

// TestSessionTopRemoteDaemonErrorKeepsEnvelope: a remote route failure must
// exit 1 and still render the daemon's code and request ID.
func TestSessionTopRemoteDaemonErrorKeepsEnvelope(t *testing.T) {
	cfg := setConfigEnv(t)
	startLocalDaemon(t, cfg, "host-local", nil)
	remote := newFakeRemoteHost(t, "host-build", "pw")
	remote.handle(http.MethodGet, "/api/v1/sessions", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"sessions":[]}`)
	})
	remote.handle(http.MethodGet, "/api/v1/usage/sessions/memory", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, http.StatusNotImplemented, map[string]string{
			"message":   "procmem: process memory is not supported on windows",
			"code":      "MEMORY_UNSUPPORTED",
			"requestId": "req-remote-7",
		})
	})
	writeRemotesFile(t, remoteEntry{Label: "build-box", URL: remote.srv.URL, Password: "pw", HostID: "host-build"})

	_, errOut, err := executeCLI(t, aliveDeps(), "session", "top", "--host", "build-box")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := ExitCode(err); got != 1 {
		t.Fatalf("exit code = %d, want 1", got)
	}
	for _, want := range []string{"MEMORY_UNSUPPORTED", "req-remote-7"} {
		if !strings.Contains(errOut+err.Error(), want) {
			t.Fatalf("error should carry %q from the remote envelope: %v\n%s", want, err, errOut)
		}
	}
}

// TestSessionTopHostSelectionErrorsAreUsageErrors matches the other --host
// commands: an unusable selector is misuse (exit 2) and sends no request.
func TestSessionTopHostSelectionErrorsAreUsageErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		entries  []remoteEntry
		selector string
		wantErr  string
	}{
		{name: "no saved hosts", selector: "build-box", wantErr: "no saved remote hosts"},
		{
			name:     "unknown label",
			entries:  []remoteEntry{{Label: "build-box", URL: "http://10.0.0.5:3001", HostID: "host-build", Password: "pw"}},
			selector: "nope",
			wantErr:  `--host "nope" matches no saved host`,
		},
		{
			name: "ambiguous label",
			entries: []remoteEntry{
				{Label: "Twin", URL: "http://10.0.0.7:3001", HostID: "host-a", Password: "pw"},
				{Label: "twin", URL: "http://10.0.0.8:3001", HostID: "host-b", Password: "pw"},
			},
			selector: "twin",
			wantErr:  "matches 2 saved hosts",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setConfigEnv(t)
			writeRemotesFile(t, tc.entries...)
			_, _, err := executeCLI(t, aliveDeps(), "session", "top", "--host", tc.selector)
			if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want usage error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[uint64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KB", 1536: "1.5 KB", 641728512: "612.0 MB", 2254857830: "2.1 GB"}
	for in, want := range cases {
		if got := formatBytes(in); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

// TestSessionTop_ActiveSessionShowsNoIdleTime: the activity field carries the
// raw enum ("active"), not the derived status ("working"), so an active row
// must print a dash where an idle row prints how long it has been quiet.
func TestSessionTop_ActiveSessionShowsNoIdleTime(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/sessions":
			_, _ = io.WriteString(w, `{"sessions":[`+
				`{"id":"demo-1","projectId":"demo","kind":"worker","harness":"codex","status":"working","isTerminated":false,`+
				`"activity":{"state":"active","lastActivityAt":"2026-06-02T12:00:00Z"},"createdAt":"2026-06-02T11:00:00Z","updatedAt":"2026-06-02T12:00:00Z"},`+
				sessionJSON("demo-2", "demo", "worker", "idle", false)+`]}`)
		case "/api/v1/usage/sessions/memory":
			_, _ = io.WriteString(w, `{"sessions":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "session", "top")
	if err != nil {
		t.Fatalf("session top failed: %v\nstderr=%s", err, errOut)
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "demo-1":
			if fields[len(fields)-1] != "-" || strings.Contains(line, "ago") {
				t.Fatalf("active session should show no idle time:\n%s", out)
			}
		case "demo-2":
			if !strings.HasSuffix(strings.TrimSpace(line), "ago") {
				t.Fatalf("idle session should show how long it has been idle:\n%s", out)
			}
		}
	}
}
