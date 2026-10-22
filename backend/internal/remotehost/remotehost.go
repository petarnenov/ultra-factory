// Package remotehost reads the desktop's saved remote AO hosts and makes the
// identity-checked requests that may carry their credentials. The CLI (for
// --host) and the daemon (for pulling remote worker reports) share it so the
// credential-file and transport rules cannot drift apart.
package remotehost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// IdentityTimeout bounds the unauthenticated identity probe that must succeed
// before a saved host credential is sent anywhere.
const IdentityTimeout = 5 * time.Second

// APIVersion is the identity contract version this build speaks. It matches
// the desktop's readRemoteIdentity check.
const APIVersion = 1

// Entry mirrors one saved connection in the desktop's remotes.json
// (frontend/src/main/remotes-store.ts). AO only reads this file.
type Entry struct {
	Label         string `json:"label"`
	URL           string `json:"url"`
	Password      string `json:"password"`
	HostID        string `json:"hostId,omitempty"`
	AccountUserID string `json:"accountUserId,omitempty"`
	// AllowInsecureHTTP is the per-host "Allow report pulls over plain HTTP"
	// setting: it lets the daemon send this host's password over plain http.
	AllowInsecureHTTP bool `json:"allowInsecureHTTP,omitempty"`
	// Tailscale marks a host reached at its Tailscale address and admitted on
	// this machine's tailnet identity; Password is then empty.
	Tailscale bool `json:"tailscale,omitempty"`
}

type identity struct {
	HostID     string `json:"hostId"`
	APIVersion int    `json:"apiVersion"`
}

// FilePath resolves the saved-host file: AO_REMOTES_FILE, else the shared
// ~/.ao/remotes.json. An isolated AO_DATA_DIR keeps its own remotes.json.
func FilePath() (string, error) {
	if p := strings.TrimSpace(os.Getenv("AO_REMOTES_FILE")); p != "" {
		return filepath.Abs(p)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve remotes file: %w", err)
	}
	stateDir := filepath.Join(home, ".ao")
	dir := strings.TrimSpace(os.Getenv("AO_DATA_DIR"))
	if dir == "" {
		return filepath.Join(stateDir, "remotes.json"), nil
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for _, daemonDefault := range []string{filepath.Join(stateDir, "data"), filepath.Join(stateDir, "dev", "data")} {
		if filepath.Clean(dir) == daemonDefault {
			return filepath.Join(stateDir, "remotes.json"), nil
		}
	}
	return filepath.Join(dir, "remotes.json"), nil
}

// ReadEntries loads saved hosts. A missing file is an empty list; a file
// readable by group or others is refused like the desktop refuses it.
func ReadEntries(path string) ([]Entry, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s holds connection passwords and is readable by others (mode %04o) — run: chmod 600 %s", path, info.Mode().Perm(), path)
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- fixed AO-owned credential file resolved above.
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Remotes []Entry `json:"remotes"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return parsed.Remotes, nil
}

// NormalizeURL lowercases a saved address and defaults it to http://.
func NormalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	return strings.ToLower(strings.TrimRight(raw, "/"))
}

// BaseURL validates a saved address the same way the desktop does before any
// request: HTTP(S) only, no embedded credentials, no path.
func BaseURL(raw string) (string, error) {
	u, err := url.Parse(NormalizeURL(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("remote host must use an HTTP(S) URL without embedded credentials")
	}
	return u.Scheme + "://" + u.Host, nil
}

// DirectClient returns a copy of base with redirects and environment proxies
// disabled, so a bearer is never replayed or read by a proxy (ADR 0008).
func DirectClient(base *http.Client, timeout time.Duration) http.Client {
	return directClient(base, timeout, nil)
}

// directClient builds the client behind DirectClient and ProbedClient. A
// non-nil gate guards the transport's dials.
func directClient(base *http.Client, timeout time.Duration, gate *ProbeGate) http.Client {
	var client http.Client
	if base != nil {
		client = *base
	}
	client.Timeout = timeout
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if transport, ok := client.Transport.(*http.Transport); ok {
		direct := transport.Clone()
		direct.Proxy = nil
		wrapDial(direct, gate)
		client.Transport = direct
		return client
	}
	if client.Transport != nil && !isDefaultTransport(client.Transport) {
		// A custom RoundTripper (a test stub) is kept as is: it never dials
		// through an environment proxy on its own.
		return client
	}
	direct := &http.Transport{}
	if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
		direct = defaultTransport.Clone()
	}
	direct.Proxy = nil
	wrapDial(direct, gate)
	client.Transport = direct
	return client
}

// wrapDial routes the transport's dials through routeTailnet and, when gate is
// set, through the gate. A transport with neither keeps its dialer untouched.
func wrapDial(transport *http.Transport, gate *ProbeGate) {
	if gate == nil && strings.TrimSpace(os.Getenv(TailnetSOCKSEnv)) == "" {
		return
	}
	inner := transport.DialContext
	if inner == nil {
		inner = defaultDial
	}
	dial := routeTailnet(inner)
	if gate != nil {
		dial = gate.guard(dial)
	}
	transport.DialContext = dial
}

func isDefaultTransport(rt http.RoundTripper) bool {
	return rt == http.DefaultTransport
}

// AllowHTTPEnv is the deprecated daemon-wide override for plain-http hosts.
// The per-host Entry.AllowInsecureHTTP setting replaces it.
const AllowHTTPEnv = "AO_REMOTE_REPORTS_ALLOW_HTTP"

// AllowHTTPEnvDeprecation is logged once per daemon start while AllowHTTPEnv
// is set to 1.
const AllowHTTPEnvDeprecation = AllowHTTPEnv + " is deprecated; use the per-host setting"

// tailscaleNet is Tailscale's CGNAT range (WireGuard-encrypted traffic).
var tailscaleNet = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// AllowHTTPEnvSet reports whether the deprecated environment override is on.
func AllowHTTPEnvSet() bool {
	return strings.TrimSpace(os.Getenv(AllowHTTPEnv)) == "1"
}

// UnattendedTransportAllowed reports whether the daemon may send a saved
// credential to base unattended: https, loopback, Tailscale, or opted in.
func UnattendedTransportAllowed(base string, allowInsecureHTTP bool) error {
	u, err := url.Parse(base)
	if err != nil {
		return err
	}
	if trustedWithoutOptIn(u) || allowInsecureHTTP || AllowHTTPEnvSet() {
		return nil
	}
	return fmt.Errorf("plain http to %s would send the saved password with no one acting; enable 'Allow report pulls over plain HTTP' for this host in Settings → Remote hosts, or use https or a Tailscale address", strings.ToLower(u.Hostname()))
}

// NeedsInsecureOptIn reports whether base is plain http to an address that
// is neither loopback nor Tailscale: the only case the setting changes.
func NeedsInsecureOptIn(base string) bool {
	u, err := url.Parse(base)
	return err == nil && !trustedWithoutOptIn(u)
}

// trustedWithoutOptIn is the transport that needs no per-host opt-in.
func trustedWithoutOptIn(u *url.URL) bool {
	if u.Scheme == "https" {
		return true
	}
	host := strings.ToLower(u.Hostname())
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	return host == "localhost" || IsTailnetHost(host)
}

// ReadIdentity is the only unauthenticated request made to a remote host. It
// returns the host ID the daemon at base answers with.
func ReadIdentity(ctx context.Context, client http.Client, base string) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, IdentityTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, base+"/api/v1/identity", http.NoBody)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req) // #nosec G704 -- saved, user-paired remote host; identity probe carries no credential.
	if err != nil {
		return "", fmt.Errorf("unreachable: %w", err)
	}
	defer func() {
		DrainBody(resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("identity probe returned HTTP %d", resp.StatusCode)
	}
	var id identity
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&id); err != nil {
		return "", fmt.Errorf("identity probe did not return an AO identity: %w", err)
	}
	if id.HostID == "" || id.HostID == "local" {
		return "", errors.New("identity probe returned no usable host ID")
	}
	if id.APIVersion != APIVersion {
		return "", errors.New("AO versions are incompatible; update AO on this computer and the remote host")
	}
	return id.HostID, nil
}

// drainLimit bounds how much of a response DrainBody reads to keep its
// connection reusable.
const drainLimit = 64 << 10

// DrainBody reads what a JSON decoder left of a response, such as the trailing
// newline; call it before closing the body. Closing a body before EOF makes
// the transport drop the connection, and the next request would then send the
// saved credential over a new connection that never answered the identity
// probe.
func DrainBody(body io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, drainLimit))
}

// Find returns the saved host recorded under hostID. It never matches by label
// or URL: a daemon acting without a human acts only on a paired identity.
func Find(hostID string) (Entry, bool, error) {
	if strings.TrimSpace(hostID) == "" {
		return Entry{}, false, nil
	}
	path, err := FilePath()
	if err != nil {
		return Entry{}, false, err
	}
	entries, err := ReadEntries(path)
	if err != nil {
		return Entry{}, false, err
	}
	for _, entry := range entries {
		if entry.HostID == hostID {
			return entry, true, nil
		}
	}
	return Entry{}, false, nil
}
