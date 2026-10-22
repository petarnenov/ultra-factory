package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestHostListReportsStateColumnAndJSON(t *testing.T) {
	healthy := newFakeRemoteHost(t, "host-build", "pw")
	healthy.handle(http.MethodGet, "/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	writeRemotesFile(t,
		remoteEntry{Label: "build", URL: healthy.srv.URL, Password: "pw", HostID: "host-build"},
		remoteEntry{Label: "wrong-pw", URL: healthy.srv.URL, Password: "other", HostID: "host-build"},
		remoteEntry{Label: "office", URL: "http://192.168.0.164:3011", Password: "pw", HostID: "host-office"},
	)
	out, errOut, err := executeCLI(t, aliveDeps(), "host", "ls")
	if err != nil {
		t.Fatalf("host ls: %v\nstderr=%s", err, errOut)
	}
	header := strings.Fields(strings.Split(out, "\n")[0])
	if header[len(header)-1] != "REPORTS" {
		t.Fatalf("header = %q, want REPORTS last", out)
	}
	for _, want := range []string{"ok", "refused password", "blocked (plain http)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table lacks %q:\n%s", want, out)
		}
	}
	jsonOut, _, err := executeCLI(t, aliveDeps(), "host", "ls", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Hosts []hostListItem `json:"hosts"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &payload); err != nil {
		t.Fatalf("json: %v\n%s", err, jsonOut)
	}
	got := map[string]string{}
	for _, host := range payload.Hosts {
		got[host.Label] = host.ReportsState
	}
	want := map[string]string{"build": "ok", "wrong-pw": "refused password", "office": "blocked (plain http)"}
	for label, state := range want {
		if got[label] != state {
			t.Errorf("%s reportsState = %q, want %q", label, got[label], state)
		}
	}
	for _, req := range healthy.recorded() {
		if req.path == "/healthz" && req.auth == "" {
			t.Fatal("healthz probed without the saved password")
		}
	}
}

func TestHostListBlockedHostNeverReceivesThePassword(t *testing.T) {
	writeRemotesFile(t, remoteEntry{Label: "office", URL: "http://192.168.0.164:3011", Password: "secret", HostID: "host-office"})
	out, _, err := executeCLI(t, aliveDeps(), "host", "ls", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "secret") || !strings.Contains(out, `"reportsState": "blocked (plain http)"`) {
		t.Fatalf("output = %s", out)
	}
}

func TestHostListFlagOptsAPlainHTTPHostIn(t *testing.T) {
	writeRemotesFile(t, remoteEntry{Label: "office", URL: "http://192.168.0.164:3011", Password: "pw", HostID: "host-office", AllowInsecureHTTP: true})
	out, _, err := executeCLI(t, aliveDeps(), "host", "ls", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "blocked") {
		t.Fatalf("per-host opt-in still blocked: %s", out)
	}
}
