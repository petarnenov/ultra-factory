package tailnet

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const statusJSON = `{
 "BackendState": "Running",
 "Self": {"ID": "nSelf", "HostName": "geo", "DNSName": "geo.tail.ts.net.", "TailscaleIPs": ["100.121.193.125"], "Online": true, "UserID": 1},
 "Peer": {
  "k1": {"ID": "nZ600", "HostName": "hpz600", "DNSName": "hpz600.tail.ts.net.", "TailscaleIPs": ["100.106.144.10", "fd7a::1"], "Online": true, "UserID": 1},
  "k2": {"ID": "nIMac", "HostName": "petars-imac", "DNSName": "petars-imac.tail.ts.net.", "TailscaleIPs": ["100.100.160.17"], "Online": true, "UserID": 1},
  "k3": {"ID": "nAir", "HostName": "air", "DNSName": "petars-macbook-air.tail.ts.net.", "TailscaleIPs": ["100.126.82.17"], "Online": false, "UserID": 1},
  "k4": {"ID": "nHub", "HostName": "ao-hub", "DNSName": "ao-hub.tail.ts.net.", "TailscaleIPs": ["100.116.159.91"], "Online": true, "UserID": 2, "Tags": ["tag:hub"]}
 },
 "User": {"1": {"ID": 1, "LoginName": "petar@github"}, "2": {"ID": 2, "LoginName": "tagged-devices"}},
 "CurrentTailnet": {"MagicDNSSuffix": "tail.ts.net"}
}`

func whoisJSON(name, login string, tags ...string) string {
	tagList := "null"
	if len(tags) > 0 {
		tagList = `["` + strings.Join(tags, `","`) + `"]`
	}
	return fmt.Sprintf(`{"Node": {"Name": "%s.tail.ts.net.", "ComputedName": "%s", "User": 7, "Tags": %s}, "UserProfile": {"ID": 7, "LoginName": "%s"}}`, name, name, tagList, login)
}

func runner(whois map[string]string, statusErr error) Runner {
	return func(_ context.Context, args ...string) ([]byte, error) {
		switch args[0] {
		case "status":
			if statusErr != nil {
				return nil, statusErr
			}
			return []byte(statusJSON), nil
		case "whois":
			if out, ok := whois[args[2]]; ok {
				return []byte(out), nil
			}
			return nil, errors.New("no such peer")
		}
		return nil, errors.New("unexpected call")
	}
}

func TestReadStatusParsesPeersAndSelfLogin(t *testing.T) {
	st, err := ReadStatus(context.Background(), runner(nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if st.SelfLogin != "petar@github" || st.Self.Address != "100.121.193.125" || st.Self.Name != "geo" {
		t.Fatalf("self = %+v login %q", st.Self, st.SelfLogin)
	}
	if len(st.Peers) != 4 || st.Peers[0].Name != "ao-hub" || st.Peers[3].Name != "petars-macbook-air" {
		t.Fatalf("peers = %+v", st.Peers)
	}
	if st.Peers[1].Address != "100.106.144.10" || st.Peers[1].DNSName != "hpz600.tail.ts.net" {
		t.Fatalf("z600 = %+v", st.Peers[1])
	}
	if len(st.Peers[0].Tags) != 1 || st.Peers[3].Online {
		t.Fatalf("hub = %+v air = %+v", st.Peers[0], st.Peers[3])
	}
}

func TestReadStatusFailsClosedWhenBackendStopped(t *testing.T) {
	run := func(context.Context, ...string) ([]byte, error) {
		return []byte(`{"BackendState": "NeedsLogin", "Self": {"ID": "x"}}`), nil
	}
	if _, err := ReadStatus(context.Background(), run); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if _, err := ReadStatus(context.Background(), runner(nil, errors.New("exec: not found"))); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestAuthorizePolicy(t *testing.T) {
	whois := map[string]string{
		"100.106.144.10": whoisJSON("hpz600", "petar@github"),
		"100.99.0.2":     whoisJSON("other", "someone@example"),
		"100.116.159.91": whoisJSON("ao-hub", "tagged-devices", "tag:hub"),
	}
	cases := []struct {
		name   string
		policy Policy
		src    string
		want   error
	}{
		{"same user allowed", Policy{}, "100.106.144.10", nil},
		{"other user rejected", Policy{}, "100.99.0.2", ErrForbidden},
		{"other user allowlisted", Policy{AllowUsers: []string{"Someone@Example"}}, "100.99.0.2", nil},
		{"tagged device rejected", Policy{}, "100.116.159.91", ErrForbidden},
		{"tagged device allowlisted", Policy{AllowTags: []string{"tag:hub"}}, "100.116.159.91", nil},
		{"tag allowlist does not admit users", Policy{AllowTags: []string{"tag:hub"}}, "100.99.0.2", ErrForbidden},
		{"unknown peer fails closed", Policy{}, "100.77.0.1", ErrUnavailable},
		{"lan source is not tailnet", Policy{}, "192.168.1.20", ErrNotTailnet},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := NewAuthorizer(runner(whois, nil), tc.policy)
			_, err := a.Authorize(context.Background(), tc.src)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestAuthorizeFailsClosedWithoutSelfLogin(t *testing.T) {
	whois := map[string]string{"100.106.144.10": whoisJSON("hpz600", "petar@github")}
	a := NewAuthorizer(runner(whois, errors.New("tailscaled down")), Policy{})
	if _, err := a.Authorize(context.Background(), "100.106.144.10"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestAuthorizeCachesSelfLogin(t *testing.T) {
	calls := 0
	whois := map[string]string{"100.106.144.10": whoisJSON("hpz600", "petar@github")}
	base := runner(whois, nil)
	run := func(ctx context.Context, args ...string) ([]byte, error) {
		if args[0] == "status" {
			calls++
		}
		return base(ctx, args...)
	}
	a := NewAuthorizer(run, Policy{})
	for i := 0; i < 3; i++ {
		if _, err := a.Authorize(context.Background(), "100.106.144.10"); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("status calls = %d", calls)
	}
}

func newDiscovery(t *testing.T, run Runner, probe Prober, ports ...int) *Discovery {
	t.Helper()
	return NewDiscovery(DiscoveryDeps{
		Run: run, Probe: probe, SelfHostID: "h_self",
		SiblingHostIDs: func() []string { return []string{"h_sibling"} },
		Ports:          ports, PrefsPath: filepath.Join(t.TempDir(), "hosts.json"),
		Now: func() time.Time { return time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC) },
	})
}

func identities(byBase map[string]string) Prober {
	return func(_ context.Context, base string) (string, error) {
		if id, ok := byBase[base]; ok {
			return id, nil
		}
		return "", errors.New("connection refused")
	}
}

func TestDiscoveryListsOnlinePeersThatAnswerOnAProbedPort(t *testing.T) {
	probe := identities(map[string]string{
		"http://100.106.144.10:3011":  "h_z600",
		"http://100.100.160.17:57451": "h_imac",
		"http://100.116.159.91:3011":  "h_sibling",
	})
	d := newDiscovery(t, runner(nil, nil), probe, 3011, 57451)
	snap := d.Refresh(context.Background())
	if !snap.Available || snap.SelfLogin != "petar@github" {
		t.Fatalf("snapshot = %+v", snap)
	}
	if len(snap.Hosts) != 2 {
		t.Fatalf("hosts = %+v", snap.Hosts)
	}
	z600, imac := snap.Hosts[0], snap.Hosts[1]
	if z600.HostID != "h_z600" || z600.URL != "http://100.106.144.10:3011" || !z600.Online || z600.Label != "hpz600" {
		t.Fatalf("z600 = %+v", z600)
	}
	if imac.HostID != "h_imac" || imac.Port != 57451 || imac.Label != "petars-imac" {
		t.Fatalf("imac = %+v", imac)
	}
	if got := snap.LocalHostIDs; len(got) != 2 || got[0] != "h_self" || got[1] != "h_sibling" {
		t.Fatalf("local = %v", got)
	}
}

func TestDiscoveryNeverListsSelfOrSiblings(t *testing.T) {
	probe := identities(map[string]string{"http://100.106.144.10:3011": "h_self", "http://100.100.160.17:3011": "h_sibling"})
	d := newDiscovery(t, runner(nil, nil), probe)
	if snap := d.Refresh(context.Background()); len(snap.Hosts) != 0 {
		t.Fatalf("hosts = %+v", snap.Hosts)
	}
}

func TestDiscoveryKeepsAKnownHostOfflineUntilItsNodeLeaves(t *testing.T) {
	answers := map[string]string{"http://100.106.144.10:3011": "h_z600"}
	run := runner(nil, nil)
	d := newDiscovery(t, run, identities(answers))
	d.Refresh(context.Background())
	delete(answers, "http://100.106.144.10:3011")
	snap := d.Refresh(context.Background())
	if len(snap.Hosts) != 1 || snap.Hosts[0].Online {
		t.Fatalf("hosts after remote access off = %+v", snap.Hosts)
	}
	if _, ok := d.Find("h_z600"); !ok {
		t.Fatal("offline host not findable")
	}
	answers["http://100.106.144.10:3011"] = "h_z600"
	if snap := d.Refresh(context.Background()); len(snap.Hosts) != 1 || !snap.Hosts[0].Online {
		t.Fatalf("hosts after remote access on = %+v", snap.Hosts)
	}
	d.deps.Run = func(_ context.Context, args ...string) ([]byte, error) {
		return []byte(`{"BackendState":"Running","Self":{"ID":"nSelf","UserID":1},"Peer":{},"User":{"1":{"LoginName":"petar@github"}}}`), nil
	}
	if snap := d.Refresh(context.Background()); len(snap.Hosts) != 0 {
		t.Fatalf("hosts after node removed = %+v", snap.Hosts)
	}
}

func TestDiscoveryReportsTailscaleUnavailable(t *testing.T) {
	answers := map[string]string{"http://100.106.144.10:3011": "h_z600"}
	d := newDiscovery(t, runner(nil, nil), identities(answers))
	d.Refresh(context.Background())
	d.deps.Run = runner(nil, errors.New("tailscaled is not running"))
	snap := d.Refresh(context.Background())
	if snap.Available || !strings.Contains(snap.Error, "tailscaled is not running") {
		t.Fatalf("snapshot = %+v", snap)
	}
	if len(snap.Hosts) != 1 || snap.Hosts[0].Online {
		t.Fatalf("hosts = %+v", snap.Hosts)
	}
}

func TestDiscoveryHiddenAndLabelPrefs(t *testing.T) {
	d := newDiscovery(t, runner(nil, nil), identities(map[string]string{"http://100.106.144.10:3011": "h_z600"}))
	d.Refresh(context.Background())
	if err := d.SetHidden("h_z600", true); err != nil {
		t.Fatal(err)
	}
	if err := d.SetLabel("h_z600", "Z600"); err != nil {
		t.Fatal(err)
	}
	host, ok := d.Find("h_z600")
	if !ok || !host.Hidden || host.Label != "Z600" {
		t.Fatalf("host = %+v", host)
	}
	prefs, err := LoadPrefs(d.deps.PrefsPath)
	if err != nil || len(prefs.Hidden) != 1 || prefs.Labels["h_z600"] != "Z600" {
		t.Fatalf("prefs = %+v err %v", prefs, err)
	}
	if err := d.SetHidden("h_z600", false); err != nil {
		t.Fatal(err)
	}
	if err := d.SetLabel("h_z600", ""); err != nil {
		t.Fatal(err)
	}
	if host, _ := d.Find("h_z600"); host.Hidden || host.Label != "hpz600" {
		t.Fatalf("host = %+v", host)
	}
}

func TestSiblingHostIDsSkipsOwnDataDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeIdentity := func(dir, id string) {
		t.Helper()
		path := filepath.Join(home, ".ao", dir, "mobile", "identity.json")
		if err := mkdirAllAndWrite(path, `{"hostId":"`+id+`"}`); err != nil {
			t.Fatal(err)
		}
	}
	writeIdentity("data", "h_prod")
	writeIdentity(filepath.Join("dev", "data"), "h_dev")
	got := SiblingHostIDs(filepath.Join(home, ".ao", "dev", "data"))
	if len(got) != 1 || got[0] != "h_prod" {
		t.Fatalf("siblings = %v", got)
	}
	if got := SiblingHostIDs(filepath.Join(home, "elsewhere")); len(got) != 2 {
		t.Fatalf("siblings = %v", got)
	}
}

func TestTailscaleBinaryFallsBackToMacAppBundle(t *testing.T) {
	missing := func(string) (string, error) { return "", errors.New("not found") }
	onPath := func(string) (string, error) { return "/usr/bin/tailscale", nil }
	if got := tailscaleBinary(onPath, "darwin", func(string) bool { return true }); got != "/usr/bin/tailscale" {
		t.Fatalf("PATH binary ignored: %s", got)
	}
	if got := tailscaleBinary(missing, "darwin", func(p string) bool { return p == macAppCLI }); got != macAppCLI {
		t.Fatalf("app bundle not used: %s", got)
	}
	if got := tailscaleBinary(missing, "linux", func(string) bool { return true }); got != "tailscale" {
		t.Fatalf("linux fallback = %s", got)
	}
}
