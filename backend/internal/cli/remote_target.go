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

// remoteTarget is a saved host whose identity has been verified for this
// invocation. Requests to it carry the saved credential and expected host ID.
type remoteTarget struct {
	label    string
	baseURL  string
	hostID   string
	password string
}

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

// localContext returns a view of c that always talks to this machine's
// daemon, for reading local context (such as the orchestrator's own project)
// while the command itself targets a remote host.
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
		return remoteEntry{}, usageError{fmt.Errorf("--host %q: no saved remote hosts; add one in the desktop app under Settings → Remote hosts", selector)}
	}
	return remoteEntry{}, usageError{fmt.Errorf("--host %q matches no saved host (saved: %s)", selector, remoteLabels(entries))}
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
// once per invocation. The saved password is sent only after the host proves
// it is the paired machine and not this machine's own daemon.
func (c *commandContext) resolveRemoteTarget(ctx context.Context) (*remoteTarget, error) {
	if c.remote != nil {
		return c.remote, nil
	}
	path, err := remotesFilePath()
	if err != nil {
		return nil, err
	}
	entries, err := readRemoteEntries(path)
	if err != nil {
		return nil, err
	}
	entry, err := selectRemoteEntry(entries, c.hostSelector)
	if err != nil {
		return nil, err
	}
	if entry.HostID == "" {
		return nil, fmt.Errorf("remote host %q must be paired again in the desktop app to record its identity", entry.Label)
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
	c.remote = &remoteTarget{label: entry.Label, baseURL: base, hostID: entry.HostID, password: entry.Password}
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
