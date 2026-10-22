package remotehost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeEntries(t *testing.T, mode os.FileMode, entries ...Entry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "remotes.json")
	raw, err := json.Marshal(map[string]any{"remotes": entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AO_REMOTES_FILE", path)
	return path
}

func TestFindMatchesOnlyThePairedHostID(t *testing.T) {
	writeEntries(t, 0o600,
		Entry{Label: "host-build", URL: "http://decoy:1", HostID: "host-decoy"},
		Entry{Label: "build-box", URL: "http://10.0.0.5:3001", Password: "pw", HostID: "host-build"},
	)
	entry, ok, err := Find("host-build")
	if err != nil || !ok || entry.Label != "build-box" {
		t.Fatalf("entry=%+v ok=%v err=%v", entry, ok, err)
	}
	for _, selector := range []string{"build-box", "http://10.0.0.5:3001", ""} {
		if _, ok, err := Find(selector); ok || err != nil {
			t.Fatalf("Find(%q) matched by something other than host ID (ok=%v err=%v)", selector, ok, err)
		}
	}
}

func TestFindRefusesACredentialFileReadableByOthers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	writeEntries(t, 0o644, Entry{Label: "build-box", HostID: "host-build"})
	if _, _, err := Find("host-build"); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadIdentityRejectsUnusableAnswers(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		want   string
	}{
		"ok":            {http.StatusOK, `{"hostId":"host-build","apiVersion":1}`, ""},
		"local":         {http.StatusOK, `{"hostId":"local","apiVersion":1}`, "no usable host ID"},
		"version":       {http.StatusOK, `{"hostId":"host-build","apiVersion":2}`, "incompatible"},
		"not ao":        {http.StatusOK, `<html>`, "did not return an AO identity"},
		"unimplemented": {http.StatusNotImplemented, `{}`, "HTTP 501"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" {
					t.Error("identity probe carried a credential")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			id, err := ReadIdentity(context.Background(), DirectClient(nil, IdentityTimeout), srv.URL)
			if tc.want == "" {
				if err != nil || id != "host-build" {
					t.Fatalf("id=%q err=%v", id, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestDirectClientNeverFollowsRedirectsOrProxies(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("redirect followed")
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()
	client := DirectClient(nil, IdentityTimeout)
	resp, err := client.Get(redirector.URL)
	if err != nil {
		t.Fatalf("request through a proxy or redirect: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want the unfollowed 302", resp.StatusCode)
	}
}

func TestUnattendedTransportAllowedPolicyMatrix(t *testing.T) {
	// Scheme × address class × per-host flag × deprecated env override.
	bases := map[string]bool{ // base → allowed without any opt-in
		"https://build.example":         true,
		"http://127.0.0.1:3001":         true,
		"http://localhost:3001":         true,
		"http://100.101.102.103:3001":   true,
		"http://build-box.tail1.ts.net": true,
		"http://192.168.0.164:3011":     false,
		"http://100.128.0.1:3001":       false,
		"http://build.example":          false,
	}
	for _, env := range []string{"", "1"} {
		t.Setenv(AllowHTTPEnv, env)
		for base, trusted := range bases {
			for _, flag := range []bool{false, true} {
				err := UnattendedTransportAllowed(base, flag)
				want := trusted || flag || env == "1"
				if (err == nil) != want {
					t.Errorf("%s flag=%v env=%q: err = %v, want allowed=%v", base, flag, env, err, want)
				}
				if err != nil && !strings.Contains(err.Error(), "Allow report pulls over plain HTTP") {
					t.Errorf("%s: refusal does not name the setting: %v", base, err)
				}
			}
		}
	}
}

func TestNeedsInsecureOptInOnlyForPlainHTTPOffLoopbackAndTailnet(t *testing.T) {
	for base, want := range map[string]bool{
		"https://build.example":         false,
		"http://127.0.0.1:3001":         false,
		"http://build-box.tail1.ts.net": false,
		"http://100.106.144.10:3011":    false,
		"http://192.168.0.164:3011":     true,
		"http://build.example":          true,
	} {
		if got := NeedsInsecureOptIn(base); got != want {
			t.Errorf("NeedsInsecureOptIn(%q) = %v, want %v", base, got, want)
		}
	}
}

func TestEntryWithoutAllowInsecureHTTPLoadsUnchanged(t *testing.T) {
	path := writeEntries(t, 0o600, Entry{Label: "office", URL: "http://192.168.0.164:3011", Password: "pw", HostID: "h1"})
	entries, err := ReadEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].AllowInsecureHTTP {
		t.Fatalf("entries = %+v, want one entry with the flag off", entries)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "allowInsecureHTTP") {
		t.Fatalf("an unedited remotes.json gained the field: %s", raw)
	}
}
