package cli

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const limitsView = `{"sessionId":"demo-c1","projectId":"demo","kind":"coordinator","ownerSessionId":null,"rootSessionId":"demo-root","workerLimit":2,"effectiveWorkerLimit":2,"effectivePlacementLimit":2,"hostSlots":[{"hostId":"host-linux-id","slots":1,"used":0}]}`

// limitsRemotes are the saved hosts the --hosts lookup resolves against.
func limitsRemotes(t *testing.T) {
	t.Helper()
	writeRemotesFile(t,
		remoteEntry{Label: "Linux", URL: "http://linux.invalid:3001", Password: "p", HostID: "host-linux-id"},
		remoteEntry{Label: "Z600", URL: "http://z600.invalid:3001", Password: "p", HostID: "host-z600-id"},
		remoteEntry{Label: "Unpaired", URL: "http://old.invalid:3001", Password: "p"},
	)
}

func TestParseHostSlotsResolvesLabelsAndIDs(t *testing.T) {
	entries := []remoteEntry{
		{Label: "Linux", HostID: "host-linux-id"},
		{Label: "Z600", HostID: "host-z600-id"},
	}
	got, err := parseHostSlots("Linux:2, host-z600-id:1", entries)
	if err != nil {
		t.Fatalf("parseHostSlots: %v", err)
	}
	want := []hostSlotInput{{HostID: "host-linux-id", Slots: 2}, {HostID: "host-z600-id", Slots: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("slots = %+v, want %+v", got, want)
	}
}

func TestParseHostSlotsRejectsMalformedAndUnknownHosts(t *testing.T) {
	entries := []remoteEntry{
		{Label: "Linux", HostID: "host-linux-id"},
		{Label: "Unpaired"},
	}
	for _, tc := range []struct {
		name, spec, want string
	}{
		{"no slots", "Linux", "want <host>:<slots>"},
		{"empty slots", "Linux:", "want <host>:<slots>"},
		{"empty host", ":2", "want <host>:<slots>"},
		{"trailing comma", "Linux:1,", "want <host>:<slots>"},
		{"zero slots", "Linux:0", "whole number of at least 1"},
		{"text slots", "Linux:many", "whole number of at least 1"},
		{"duplicate host", "Linux:1,Linux:2", "listed more than once"},
		{"unknown host", "Mars:1", "matches no remote host"},
		{"unpaired host", "Unpaired:1", "must be paired again"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseHostSlots(tc.spec, entries)
			var usage usageError
			if !errors.As(err, &usage) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want usage error containing %q", err, tc.want)
			}
		})
	}
}

func TestCoordinatorSetLimitsSendsLimitHostsAndCaller(t *testing.T) {
	for _, tc := range []struct {
		name      string
		args      []string
		wantLimit *int
		wantSlots *[]hostSlotInput
	}{
		{
			name:      "limit and hosts",
			args:      []string{"--max-workers", "2", "--hosts", "Linux:1"},
			wantLimit: intPtr(2),
			wantSlots: &[]hostSlotInput{{HostID: "host-linux-id", Slots: 1}},
		},
		{
			name:      "limit only keeps stored slots",
			args:      []string{"--max-workers", "1"},
			wantLimit: intPtr(1),
		},
		{
			name:      "empty hosts clears the slots",
			args:      []string{"--hosts", ""},
			wantSlots: &[]hostSlotInput{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limitsRemotes(t)
			srv, capture := ownershipServer(t, http.StatusOK, limitsView)
			t.Setenv("AO_SESSION_ID", "demo-root")

			out, errOut, err := runOwnershipCLI(t, srv, append([]string{"coordinator", "set-limits", "demo-c1"}, tc.args...)...)
			if err != nil {
				t.Fatalf("coordinator set-limits: %v\nstderr=%s", err, errOut)
			}
			if capture.method != http.MethodPut || capture.path != "/api/v1/sessions/demo-c1/coordinator-limits" {
				t.Fatalf("request = %s %s", capture.method, capture.path)
			}
			var got struct {
				WorkerLimit          *int             `json:"workerLimit"`
				HostSlots            *[]hostSlotInput `json:"hostSlots"`
				RequestedBySessionID string           `json:"requestedBySessionId"`
			}
			if err := json.Unmarshal([]byte(capture.body), &got); err != nil {
				t.Fatalf("body %q: %v", capture.body, err)
			}
			if got.RequestedBySessionID != "demo-root" {
				t.Fatalf("requestedBy = %q", got.RequestedBySessionID)
			}
			if !reflect.DeepEqual(got.WorkerLimit, tc.wantLimit) || !reflect.DeepEqual(got.HostSlots, tc.wantSlots) {
				t.Fatalf("limit=%v slots=%v, want limit=%v slots=%v", derefInt(got.WorkerLimit), got.HostSlots, derefInt(tc.wantLimit), tc.wantSlots)
			}
			if !strings.Contains(out, "coordinator demo-c1 limit: 2  hosts: Linux 0/1") {
				t.Fatalf("output = %q", out)
			}
		})
	}
}

func TestCoordinatorSetLimitsUsageErrorsExitTwoWithoutContactingTheDaemon(t *testing.T) {
	limitsRemotes(t)
	for _, tc := range []struct {
		name, session string
		args          []string
		want          string
	}{
		{"no flags", "demo-root", nil, "pass --max-workers, --hosts, or both"},
		{"zero limit", "demo-root", []string{"--max-workers", "0"}, "--max-workers must be at least 1"},
		{"no caller", "", []string{"--max-workers", "1"}, "requested-by session id is required"},
		{"unknown host", "demo-root", []string{"--hosts", "Mars:1"}, "matches no remote host"},
		{"malformed hosts", "demo-root", []string{"--hosts", "Linux"}, "want <host>:<slots>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, capture := ownershipServer(t, http.StatusOK, limitsView)
			t.Setenv("AO_SESSION_ID", tc.session)
			_, _, err := runOwnershipCLI(t, srv, append([]string{"coordinator", "set-limits", "demo-c1"}, tc.args...)...)
			var usage usageError
			if !errors.As(err, &usage) || ExitCode(err) != 2 || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v exit=%d, want usage error containing %q", err, ExitCode(err), tc.want)
			}
			if capture.method != "" {
				t.Fatalf("daemon was contacted: %s %s", capture.method, capture.path)
			}
		})
	}
}

func TestCoordinatorSetLimitsDaemonRefusalExitsOneWithTheCode(t *testing.T) {
	srv, _ := ownershipServer(t, http.StatusBadRequest,
		`{"error":"bad_request","code":"COORDINATOR_WORKER_LIMIT_INVALID","message":"workerLimit exceeds the project maximum","requestId":"req-4"}`)
	t.Setenv("AO_SESSION_ID", "demo-root")

	_, _, err := runOwnershipCLI(t, srv, "coordinator", "set-limits", "demo-c1", "--max-workers", "9")
	if err == nil || ExitCode(err) != 1 {
		t.Fatalf("err=%v exit=%d, want daemon error 1", err, ExitCode(err))
	}
	for _, want := range []string{"COORDINATOR_WORKER_LIMIT_INVALID", "request req-4"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestCoordinatorListShowsLimitsHostUsageAndProfiles(t *testing.T) {
	limitsRemotes(t)
	srv, capture := ownershipServer(t, http.StatusOK,
		`{"projectId":"demo","rootSessionId":"demo-root","coordinators":[{"coordinatorSessionId":"demo-c1","coordinatorScope":"auth","isTerminated":false,"workerSessionIds":["demo-1"],"workerCount":1,"workerLimit":2,"effectiveWorkerLimit":2,"activeWorkerCount":1,"effectivePlacementLimit":2,"hostSlots":[{"hostId":"host-linux-id","slots":1,"used":0},{"hostId":"host-z600-id","slots":1,"used":1}],"agentProfileId":null,"workerAgent":{"mode":"fixed","profileId":"fast"},"workers":[{"sessionId":"demo-1","agentProfileId":"fast"}]}],"rootWorkerSessionIds":[]}`)

	out, errOut, err := runOwnershipCLI(t, srv, "coordinator", "ls", "--project", "demo")
	if err != nil {
		t.Fatalf("coordinator ls failed: %v\nstderr=%s", err, errOut)
	}
	if capture.path != "/api/v1/projects/demo/coordinators" {
		t.Fatalf("request path = %s", capture.path)
	}
	for _, want := range []string{
		"coordinator demo-c1 (scope: auth)  workers: 1",
		"limit: 2",
		"hosts: Linux 0/1, Z600 1/1",
		"worker agent: fixed:fast",
		"demo-1  (profile: fast)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q missing %q", out, want)
		}
	}
}

func TestCoordinatorSetLimitsJSONCarriesTheView(t *testing.T) {
	limitsRemotes(t)
	srv, _ := ownershipServer(t, http.StatusOK, limitsView)
	t.Setenv("AO_SESSION_ID", "demo-root")

	out, _, err := runOwnershipCLI(t, srv, "coordinator", "set-limits", "demo-c1", "--max-workers", "2", "--json")
	if err != nil {
		t.Fatalf("set-limits --json: %v", err)
	}
	var got sessionOwnershipDTO
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output %q: %v", out, err)
	}
	if got.EffectivePlacementLimit == nil || *got.EffectivePlacementLimit != 2 || len(got.HostSlots) != 1 {
		t.Fatalf("view = %+v", got)
	}
}

func TestSpawnCoordinatorSendsWorkerLimitAndHostSlots(t *testing.T) {
	cfg := setConfigEnv(t)
	limitsRemotes(t)
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
		"spawn", "--project", "demo", "--agent", "codex", "--skip-agent-check", "--name", "auth", "--kind", "coordinator", "--scope", "auth",
		"--max-workers", "2", "--hosts", "Linux:1")
	if err != nil {
		t.Fatalf("spawn failed: %v stderr=%s", err, errOut)
	}
	if req.WorkerLimit == nil || *req.WorkerLimit != 2 {
		t.Fatalf("workerLimit = %v, want 2", derefInt(req.WorkerLimit))
	}
	want := []hostSlotInput{{HostID: "host-linux-id", Slots: 1}}
	if !reflect.DeepEqual(req.HostSlots, want) {
		t.Fatalf("hostSlots = %+v, want %+v", req.HostSlots, want)
	}
}

func TestSpawnLimitFlagsAreUsageErrorsOutsideCoordinators(t *testing.T) {
	limitsRemotes(t)
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"max-workers on a worker", []string{"--max-workers", "2"}, "--max-workers and --hosts are supported with --kind coordinator only"},
		{"hosts on an orchestrator", []string{"--kind", "orchestrator", "--hosts", "Linux:1"}, "--max-workers and --hosts are supported with --kind coordinator only"},
		{"zero max-workers", []string{"--kind", "coordinator", "--scope", "auth", "--max-workers", "0"}, "--max-workers must be at least 1"},
		{"unknown host", []string{"--kind", "coordinator", "--scope", "auth", "--hosts", "Mars:1"}, "matches no remote host"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"spawn", "--project", "demo", "--name", "coord"}, tc.args...)
			_, _, err := executeCLI(t, Deps{}, args...)
			if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v exit=%d, want usage error containing %q", err, ExitCode(err), tc.want)
			}
		})
	}
}

func intPtr(v int) *int { return &v }

func derefInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}
