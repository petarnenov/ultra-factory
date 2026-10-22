package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
)

// remoteIdentityTimeout bounds the unauthenticated identity probe that must
// succeed before a saved host credential is sent anywhere.
const remoteIdentityTimeout = remotehost.IdentityTimeout

// remoteEntry is one saved connection in the desktop's remotes.json.
type remoteEntry = remotehost.Entry

// remoteTarget is a host whose identity has been verified for this invocation.
// Requests carry the expected host ID and, outside Tailscale, the credential.
type remoteTarget struct {
	label    string
	baseURL  string
	hostID   string
	password string
	// tailscale marks a discovered host admitted on this machine's tailnet
	// identity: no bearer is sent, and a rejection is an identity problem.
	tailscale bool
}

// tailnetHostsResponse mirrors the daemon's GET /api/v1/tailnet/hosts.
type tailnetHostsResponse struct {
	Available    bool          `json:"available"`
	Error        string        `json:"error"`
	LocalHostIDs []string      `json:"localHostIds"`
	Hosts        []tailnetHost `json:"hosts"`
}

type tailnetHost struct {
	HostID  string `json:"hostId"`
	Label   string `json:"label"`
	Name    string `json:"name"`
	Address string `json:"address"`
	URL     string `json:"url"`
	Online  bool   `json:"online"`
	Hidden  bool   `json:"hidden"`
}

// hostDirectory merges the hosts this machine's daemon found on the tailnet
// with the desktop's saved hosts; a discovered host wins on the same host ID.
type hostDirectory struct {
	entries []remoteEntry
	// local are saved entries that point at this machine or a sibling install;
	// they are never listed, but --host names one with a clear error.
	local []remoteEntry
	// online is the discovery state of each Tailscale entry.
	online map[string]bool
	// tailscale is the raw discovery answer; discoveryErr is set when this
	// machine's daemon could not be asked at all.
	tailscale    tailnetHostsResponse
	discoveryErr error
}

func (c *commandContext) loadHostDirectory(ctx context.Context) (hostDirectory, error) {
	path, err := remotesFilePath()
	if err != nil {
		return hostDirectory{}, err
	}
	saved, err := readRemoteEntries(path)
	if err != nil {
		return hostDirectory{}, err
	}
	dir := hostDirectory{online: map[string]bool{}}
	reqCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	dir.discoveryErr = c.localContext().doJSONPathWithHeadersAndTimeout(reqCtx, http.MethodGet, "/api/v1/tailnet/hosts", nil, &dir.tailscale, nil, probeTimeout)
	local := map[string]bool{}
	for _, id := range dir.tailscale.LocalHostIDs {
		local[id] = true
	}
	if localID, err := c.localContext().readLocalIdentity(ctx); err == nil && localID != "" {
		local[localID] = true
	}
	seen := map[string]bool{}
	discovered := map[string]bool{}
	for _, host := range dir.tailscale.Hosts {
		if host.Hidden || host.HostID == "" || local[host.HostID] || seen[host.HostID] {
			continue
		}
		seen[host.HostID] = true
		discovered[host.HostID] = true
		dir.online[host.HostID] = host.Online
		dir.entries = append(dir.entries, remoteEntry{Label: host.Label, URL: host.URL, HostID: host.HostID, Tailscale: true})
	}
	for _, entry := range saved {
		if entry.HostID != "" && local[entry.HostID] {
			dir.local = append(dir.local, entry)
			continue
		}
		if entry.HostID != "" && discovered[entry.HostID] {
			continue
		}
		dir.entries = append(dir.entries, entry)
	}
	return dir, nil
}

// noHostsHint tells the user how a host appears in `ao host ls`.
const noHostsHint = "turn on remote access on another machine on your tailnet (`ao remote-host enable`), or pair a host outside Tailscale in the desktop app under Settings → Remote hosts"

type remoteIdentity struct {
	HostID     string `json:"hostId"`
	APIVersion int    `json:"apiVersion"`
}

// addHostFlag opts a product command into --host. Only commands registered
// through this helper can reach a remote daemon; everything else stays local.
func addHostFlag(cmd *cobra.Command, ctx *commandContext) {
	cmd.Flags().StringVar(&ctx.hostSelector, "host", "",
		"Run against a saved remote AO host (label, host ID, or URL listed by 'ao host ls') instead of this machine")
}

func (c *commandContext) targetsRemote() bool {
	return strings.TrimSpace(c.hostSelector) != ""
}

// localContext returns a view of c that always talks to this machine's daemon,
// for reading local context while the command itself targets a remote host.
func (c *commandContext) localContext() *commandContext {
	return &commandContext{deps: c.deps}
}

// remotesFilePath resolves the desktop's saved-host file.
func remotesFilePath() (string, error) { return remotehost.FilePath() }

// readRemoteEntries loads saved hosts, refusing a file readable by others.
func readRemoteEntries(path string) ([]remoteEntry, error) { return remotehost.ReadEntries(path) }

// selectRemoteEntry matches a selector against host ID, then label
// (case-insensitive), then URL. It never guesses between several matches.
func selectRemoteEntry(entries []remoteEntry, selector string) (remoteEntry, error) {
	selector = strings.TrimSpace(selector)
	matchers := []func(remoteEntry) bool{
		func(e remoteEntry) bool { return e.HostID != "" && e.HostID == selector },
		func(e remoteEntry) bool { return strings.EqualFold(strings.TrimSpace(e.Label), selector) },
		func(e remoteEntry) bool { return normalizeRemoteURL(e.URL) == normalizeRemoteURL(selector) },
	}
	for _, match := range matchers {
		var found []remoteEntry
		for _, entry := range entries {
			if match(entry) {
				found = append(found, entry)
			}
		}
		if len(found) == 1 {
			return found[0], nil
		}
		if len(found) > 1 {
			return remoteEntry{}, usageError{fmt.Errorf("--host %q matches %d saved hosts (%s); pass a host ID from `ao host ls`", selector, len(found), remoteLabels(found))}
		}
	}
	if len(entries) == 0 {
		return remoteEntry{}, usageError{fmt.Errorf("--host %q: no remote hosts; %s", selector, noHostsHint)}
	}
	return remoteEntry{}, usageError{fmt.Errorf("--host %q matches no saved host or discovered remote host (known: %s)", selector, remoteLabels(entries))}
}

func remoteLabels(entries []remoteEntry) string {
	labels := make([]string, 0, len(entries))
	for _, entry := range entries {
		labels = append(labels, entry.Label)
	}
	sort.Strings(labels)
	return strings.Join(labels, ", ")
}

func normalizeRemoteURL(raw string) string { return remotehost.NormalizeURL(raw) }

// remoteBaseURL validates a saved address before any request.
func remoteBaseURL(raw string) (string, error) { return remotehost.BaseURL(raw) }

// remoteHTTPClient returns the injected client with redirects and environment
// proxies disabled; see remotehost.DirectClient.
func (c *commandContext) remoteHTTPClient(timeout time.Duration) http.Client {
	return remotehost.DirectClient(c.deps.HTTPClient, timeout)
}

// resolveRemoteTarget loads, selects, and identity-checks the --host target
// once per invocation; a password goes only to a verified non-Tailscale host.
func (c *commandContext) resolveRemoteTarget(ctx context.Context) (*remoteTarget, error) {
	if c.remote != nil {
		return c.remote, nil
	}
	dir, err := c.loadHostDirectory(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := selectRemoteEntry(dir.local, c.hostSelector); err == nil {
		return nil, usageError{fmt.Errorf("--host %q is this machine; omit --host to use the local daemon", c.hostSelector)}
	}
	entry, err := selectRemoteEntry(dir.entries, c.hostSelector)
	if err != nil {
		return nil, err
	}
	if entry.HostID == "" {
		return nil, fmt.Errorf("remote host %q must be paired again in the desktop app to record its identity", entry.Label)
	}
	if entry.Tailscale && !dir.online[entry.HostID] {
		return nil, fmt.Errorf("remote host %q is offline on the tailnet; check that AO is running there with remote access on", entry.Label)
	}
	base, err := remoteBaseURL(entry.URL)
	if err != nil {
		return nil, err
	}
	actual, err := c.readRemoteIdentity(ctx, base)
	if err != nil {
		return nil, fmt.Errorf("remote host %q at %s: %w", entry.Label, base, err)
	}
	if actual != entry.HostID {
		return nil, fmt.Errorf("remote host identity changed for %s (saved %s, answered %s); connection refused — re-pair it in the desktop app", base, entry.HostID, actual)
	}
	if local, err := c.localContext().readLocalIdentity(ctx); err == nil && local == actual {
		return nil, usageError{fmt.Errorf("--host %q is this machine; omit --host to use the local daemon", c.hostSelector)}
	}
	c.remote = &remoteTarget{label: entry.Label, baseURL: base, hostID: entry.HostID, password: entry.Password, tailscale: entry.Tailscale}
	if entry.Tailscale {
		c.remote.password = ""
	}
	return c.remote, nil
}

// readRemoteIdentity is the only unauthenticated request made to a remote host.
func (c *commandContext) readRemoteIdentity(ctx context.Context, base string) (string, error) {
	return remotehost.ReadIdentity(ctx, c.remoteHTTPClient(remoteIdentityTimeout), base)
}

// readLocalIdentity reads this machine's host ID from the loopback daemon.
func (c *commandContext) readLocalIdentity(ctx context.Context) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	var identity remoteIdentity
	if err := c.doJSONPathWithHeadersAndTimeout(reqCtx, http.MethodGet, "/api/v1/identity", nil, &identity, nil, probeTimeout); err != nil {
		return "", err
	}
	return identity.HostID, nil
}

// remoteSenderLabel is the human-readable origin added to messages sent to a
// remote session. Session IDs are per daemon, so the sender's ID alone would
// be ambiguous (or name an unrelated session) on the receiving host.
func remoteSenderLabel(sender string) string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return sender
	}
	return sender + " on " + host
}

// shellQuote makes a --host value safe to paste back into a shell; saved
// labels may contain spaces.
func shellQuote(value string) string {
	if value != "" && strings.IndexFunc(value, func(r rune) bool { return !isShellSafe(r) }) < 0 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func isShellSafe(r rune) bool {
	switch {
	case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		return true
	default:
		return strings.ContainsRune("-_.:/", r)
	}
}
