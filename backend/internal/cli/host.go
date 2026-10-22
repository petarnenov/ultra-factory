package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// hostSource values: how a host became known to this machine.
const (
	hostSourceTailscale = "tailscale"
	hostSourceSaved     = "saved"
)

type hostListItem struct {
	Label        string `json:"label"`
	HostID       string `json:"hostId,omitempty"`
	URL          string `json:"url"`
	Status       string `json:"status"`
	Detail       string `json:"detail,omitempty"`
	ReportsState string `json:"reportsState"`
	// Source is "tailscale" for a host discovered on the tailnet (no password
	// needed) or "saved" for one paired in the desktop app.
	Source string `json:"source"`
}

// hostListOutput is the JSON shape of `ao host ls --json`.
type hostListOutput struct {
	Hosts     []hostListItem `json:"hosts"`
	Tailscale struct {
		Available bool   `json:"available"`
		Error     string `json:"error,omitempty"`
	} `json:"tailscale"`
}

func newHostCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "host",
		Short: "Remote AO hosts that --host can target",
		Long: "Remote AO hosts are the machines on your Tailscale network running AO with remote access on\n" +
			"(found automatically, no password needed) plus any host paired in the desktop app under\n" +
			"Settings → Remote hosts. Commands that accept --host (spawn, send, session, project ls|get,\n" +
			"orchestrator ls, agent ls) run against that host's daemon, so one orchestrator can delegate\n" +
			"workers to several machines.",
	}
	cmd.AddCommand(newHostListCommand(ctx))
	return cmd
}

func newHostListCommand(ctx *commandContext) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List remote hosts found on the tailnet or saved, and whether each is reachable",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := ctx.listHosts(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}
			if len(result.Hosts) == 0 {
				if !result.Tailscale.Available {
					if _, err := fmt.Fprintf(out, "Tailscale unavailable: %s\n", result.Tailscale.Error); err != nil {
						return err
					}
				}
				_, err := fmt.Fprintln(out, "No remote hosts: "+noHostsHint+".")
				return err
			}
			tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "LABEL\tHOST ID\tURL\tSTATUS\tVIA\tREPORTS")
			for _, item := range result.Hosts {
				status := item.Status
				if item.Detail != "" {
					status += " (" + item.Detail + ")"
				}
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", item.Label, item.HostID, item.URL, status, item.Source, item.ReportsState)
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			if !result.Tailscale.Available {
				_, err := fmt.Fprintf(out, "Tailscale unavailable: %s\n", result.Tailscale.Error)
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

// listHosts reports every known host without revealing a credential; a host
// is asked only after proving its identity, Tailscale hosts with no password.
func (c *commandContext) listHosts(ctx context.Context) (hostListOutput, error) {
	dir, err := c.loadHostDirectory(ctx)
	if err != nil {
		return hostListOutput{}, err
	}
	var result hostListOutput
	result.Tailscale.Available = dir.tailscale.Available
	result.Tailscale.Error = dir.tailscale.Error
	if dir.discoveryErr != nil {
		result.Tailscale.Error = "this machine's AO daemon could not be asked: " + dir.discoveryErr.Error()
	}
	localID, _ := c.localContext().readLocalIdentity(ctx)
	result.Hosts = make([]hostListItem, len(dir.entries))
	var wg sync.WaitGroup
	for i, entry := range dir.entries {
		wg.Add(1)
		go func(i int, entry remoteEntry) {
			defer wg.Done()
			result.Hosts[i] = c.probeHost(ctx, entry, localID, dir.online[entry.HostID])
		}(i, entry)
	}
	wg.Wait()
	return result, nil
}

func (c *commandContext) probeHost(ctx context.Context, entry remoteEntry, localID string, online bool) hostListItem {
	item := hostListItem{Label: entry.Label, HostID: entry.HostID, URL: entry.URL, Source: hostSourceSaved, ReportsState: reportsUnreachable}
	if entry.Tailscale {
		item.Source = hostSourceTailscale
		if !online {
			item.Status, item.Detail = hostStatusOffline, "not reachable on the tailnet"
			return item
		}
	}
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
	if !entry.Tailscale {
		req.Header.Set("Authorization", "Bearer "+entry.Password)
	}
	req.Header.Set("X-AO-Expected-Host-ID", entry.HostID)
	client := c.remoteHTTPClient(remoteIdentityTimeout)
	resp, err := client.Do(req) // #nosec G704 -- identity-verified saved host.
	if err != nil {
		item.Status, item.Detail = hostStatusOffline, err.Error()
		return item
	}
	var e apiError
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&e)
	_ = resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		item.Status, item.ReportsState = hostStatusUnauthorized, reportsRefused
		if entry.Tailscale {
			item.Detail = tailnetRejectionReason(e)
		}
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		item.Status, item.ReportsState = hostStatusOnline, reportsOK
	default:
		item.Status, item.Detail = hostStatusOffline, fmt.Sprintf("healthz returned HTTP %d", resp.StatusCode)
	}
	return item
}
