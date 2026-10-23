package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
	"github.com/aoagents/agent-orchestrator/backend/internal/tailnet"
)

type fakeTailnetFinder map[string]tailnet.Host

func (f fakeTailnetFinder) Find(hostID string) (tailnet.Host, bool) {
	host, ok := f[hostID]
	return host, ok
}

func writeSavedHosts(t *testing.T, entries ...remotehost.Entry) {
	t.Helper()
	raw, err := json.Marshal(map[string][]remotehost.Entry{"remotes": entries})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "remotes.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AO_REMOTES_FILE", path)
}

func TestRemoteHostLookupMovesASavedEntryToItsTailnetAddress(t *testing.T) {
	writeSavedHosts(t,
		remotehost.Entry{Label: "Z600 (LAN)", URL: "http://192.0.2.10:3011", Password: "pw-old", HostID: "host-z600"},
		remotehost.Entry{Label: "iMac (LAN)", URL: "http://192.0.2.11:3011", Password: "pw-imac", HostID: "host-imac"},
		remotehost.Entry{Label: "hidden-box", URL: "http://192.0.2.12:3011", Password: "pw-hidden", HostID: "host-hidden"},
	)
	lookup := remoteHostLookup(fakeTailnetFinder{
		"host-z600":   {HostID: "host-z600", Label: "hpz600", URL: "http://100.106.144.10:3011"},
		"host-hidden": {HostID: "host-hidden", Label: "hidden-box", URL: "http://100.1.1.1:3011", Hidden: true},
	})

	moved, ok, err := lookup("host-z600")
	if err != nil || !ok {
		t.Fatalf("discovered saved host: ok=%v err=%v", ok, err)
	}
	if moved.URL != "http://100.106.144.10:3011" || !moved.Tailscale || moved.Password != "" || moved.Label != "hpz600" {
		t.Fatalf("discovered host = %+v, want the tailnet URL, no saved password and the discovered label", moved)
	}

	for _, hostID := range []string{"host-imac", "host-hidden"} {
		kept, ok, err := lookup(hostID)
		if err != nil || !ok || kept.Tailscale || kept.Password == "" {
			t.Fatalf("%s = %+v ok=%v err=%v, want the saved entry untouched", hostID, kept, ok, err)
		}
	}

	if _, ok, err := lookup("host-unknown"); ok || err != nil {
		t.Fatalf("unknown host: ok=%v err=%v, want not found", ok, err)
	}
}
