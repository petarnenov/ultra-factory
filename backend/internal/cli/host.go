package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
)

// hostStatus values reported by `ao host ls`.
const (
	hostStatusOnline         = "online"
	hostStatusUnauthorized   = "unauthorized"
	hostStatusOffline        = "offline"
	hostStatusIdentityChange = "identity-changed"
	hostStatusNeedsPairing   = "needs-pairing"
	hostStatusSelf           = "this-machine"
	hostStatusNotProbed      = "not-probed"
)

// reportsState values reported by `ao host ls` for the daemon's report pulls.
const (
	reportsOK          = "ok"
	reportsBlocked     = "blocked (plain http)"
	reportsRefused     = "refused password"
	reportsUnreachable = "unreachable"
)

type hostListItem struct {
	Label        string `json:"label"`
	HostID       string `json:"hostId,omitempty"`
	URL          string `json:"url"`
	Status       string `json:"status"`
	Detail       string `json:"detail,omitempty"`
	ReportsState string `json:"reportsState"`
}

func newHostCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "host",
		Short: "Saved remote AO hosts that --host can target",
		Long: "Saved remote AO hosts are the machines paired in the desktop app under Settings → Remote hosts.\n" +
			"Commands that accept --host (spawn, send, session, project ls|get, orchestrator ls, agent ls)\n" +
			"run against that host's daemon, so one orchestrator can delegate workers to several machines.",
	}
	cmd.AddCommand(newHostListCommand(ctx))
	return cmd
}

func newHostListCommand(ctx *commandContext) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List saved remote hosts and whether each is reachable",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := ctx.listHosts(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(struct {
					Hosts []hostListItem `json:"hosts"`
				}{Hosts: items})
			}
			if len(items) == 0 {
				_, err := fmt.Fprintln(out, "No saved remote hosts. Pair one in the desktop app under Settings → Remote hosts.")
				return err
			}
			tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "LABEL\tHOST ID\tURL\tSTATUS\tREPORTS")
			for _, item := range items {
				status := item.Status
				if item.Detail != "" {
					status += " (" + item.Detail + ")"
				}
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", item.Label, item.HostID, item.URL, status, item.ReportsState)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

// listHosts reports every saved host without revealing its credential. The
// password is presented only to a host that first proves its saved identity.
func (c *commandContext) listHosts(ctx context.Context) ([]hostListItem, error) {
	path, err := remotesFilePath()
	if err != nil {
		return nil, err
	}
	entries, err := readRemoteEntries(path)
	if err != nil {
		return nil, err
	}
	localID, _ := c.localContext().readLocalIdentity(ctx)
	items := make([]hostListItem, len(entries))
	var wg sync.WaitGroup
	for i, entry := range entries {
		wg.Add(1)
		go func(i int, entry remoteEntry) {
			defer wg.Done()
			items[i] = c.probeHost(ctx, entry, localID)
		}(i, entry)
	}
	wg.Wait()
	return items, nil
}

func (c *commandContext) probeHost(ctx context.Context, entry remoteEntry, localID string) hostListItem {
	// Nothing reaches a host that needs pairing, is unreachable, or is this
	// machine, so REPORTS starts as unreachable and the probe upgrades it.
	item := hostListItem{Label: entry.Label, HostID: entry.HostID, URL: entry.URL, ReportsState: reportsUnreachable}
	if entry.HostID == "" {
		item.Status = hostStatusNeedsPairing
		return item
	}
	base, err := remoteBaseURL(entry.URL)
	if err != nil {
		item.Status, item.Detail = hostStatusOffline, err.Error()
		return item
	}
	// The daemon's policy gate runs before any request: a blocked host gets
	// neither the identity probe nor the password.
	if err := remotehost.UnattendedTransportAllowed(base, entry.AllowInsecureHTTP); err != nil {
		item.Status, item.ReportsState = hostStatusNotProbed, reportsBlocked
		return item
	}
	actual, err := c.readRemoteIdentity(ctx, base)
	switch {
	case err != nil:
		item.Status, item.Detail = hostStatusOffline, err.Error()
		return item
	case actual != entry.HostID:
		item.Status, item.Detail = hostStatusIdentityChange, "answered as "+actual
		return item
	case localID != "" && actual == localID:
		item.Status = hostStatusSelf
		return item
	}
	reqCtx, cancel := context.WithTimeout(ctx, remoteIdentityTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, base+"/healthz", http.NoBody)
	if err != nil {
		item.Status, item.Detail = hostStatusOffline, err.Error()
		return item
	}
	req.Header.Set("Authorization", "Bearer "+entry.Password)
	req.Header.Set("X-AO-Expected-Host-ID", entry.HostID)
	client := c.remoteHTTPClient(remoteIdentityTimeout)
	resp, err := client.Do(req) // #nosec G704 -- identity-verified saved host.
	if err != nil {
		item.Status, item.Detail = hostStatusOffline, err.Error()
		return item
	}
	_ = resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		item.Status, item.ReportsState = hostStatusUnauthorized, reportsRefused
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		item.Status, item.ReportsState = hostStatusOnline, reportsOK
	default:
		item.Status, item.Detail = hostStatusOffline, fmt.Sprintf("healthz returned HTTP %d", resp.StatusCode)
	}
	return item
}
