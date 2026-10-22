package cli

import (
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/spf13/cobra"
)

// remoteHostStatus mirrors the small part of the loopback mobile status API
// needed to pair another client with a headless daemon.
type remoteHostStatus struct {
	Enabled                bool   `json:"enabled"`
	LoopbackOnly           bool   `json:"loopbackOnly"`
	TailnetOnly            bool   `json:"tailnetOnly"`
	PasswordFallback       bool   `json:"passwordFallback"`
	TailnetListenerEnabled bool   `json:"tailnetListenerEnabled"`
	TailnetListenerPort    int    `json:"tailnetListenerPort"`
	HostID                 string `json:"hostId"`
	Password               string `json:"password"`
	Tunnel                 struct {
		Supported bool   `json:"supported"`
		Running   bool   `json:"running"`
		LastError string `json:"lastError"`
	} `json:"tunnel"`
	Endpoints []struct {
		Kind   string `json:"kind"`
		Host   string `json:"host"`
		Port   int    `json:"port"`
		Secure bool   `json:"secure"`
	} `json:"endpoints"`
}

func newRemoteHostCommand(ctx *commandContext) *cobra.Command {
	root := &cobra.Command{Use: "remote-host", Short: "Manage this machine's remote listener",
		Long: "By default `ao remote-host enable` listens only on this machine's Tailscale address and admits\n" +
			"callers on their Tailscale identity (your own Tailscale user; see AO_TAILNET_ALLOW_USERS and\n" +
			"AO_TAILNET_ALLOW_TAGS for others). Other machines on the tailnet find it automatically with\n" +
			"`ao host ls`. Legacy LAN hosts can opt in with `ao remote-host enable --also-tailnet`; this\n" +
			"adds identity-only discovery on the configured port (default :3011) and keeps mobile access.\n" +
			"Use --lan for the password-protected LAN listener, or --tunnel/--tunnel-only\n" +
			"for a managed Cloudflare tunnel; those keep working as before."}
	var tunnel, tunnelOnly, lan, allowPassword, alsoTailnet bool
	for _, action := range []struct {
		name, method, path string
	}{
		{"status", "GET", "mobile/status"},
		{"enable", "POST", "mobile/enable-tailnet"},
		{"disable", "POST", "mobile/disable"},
	} {
		cmd := &cobra.Command{
			Use: action.name, Args: noArgs,
			Short: action.name + " this machine's authenticated remote listener",
			RunE: func(cmd *cobra.Command, _ []string) error {
				var status remoteHostStatus
				method, path := action.method, action.path
				var body any
				if action.name == "enable" && !lan && !tunnel && !tunnelOnly {
					body = map[string]bool{"passwordFallback": allowPassword}
				}
				if action.name == "enable" && alsoTailnet {
					method, path, body = "POST", "mobile/enable-also-tailnet", nil
				} else if action.name == "enable" && lan {
					method, path = "POST", "mobile/enable-lan-only"
				} else if action.name == "enable" && tunnelOnly {
					method, path = "POST", "mobile/enable-tunnel-only"
				} else if action.name == "enable" && tunnel {
					if err := ctx.doJSON(cmd.Context(), "GET", "mobile/status", nil, &status); err != nil {
						return remoteHostDaemonError(err)
					}
					if status.Enabled && status.LoopbackOnly {
						return errors.New("listener is loopback-only; run `ao remote-host disable` before enabling LAN and tunnel access")
					}
					if !status.Enabled {
						if err := ctx.doJSON(cmd.Context(), "POST", "mobile/enable-lan-only", nil, &status); err != nil {
							return remoteHostDaemonError(err)
						}
					}
					method, path = "POST", "mobile/remote-access"
				}
				err := ctx.doJSON(cmd.Context(), method, path, body, &status)
				if err != nil {
					return remoteHostDaemonError(err)
				}
				if !status.Enabled {
					_, err := fmt.Fprintln(cmd.OutOrStdout(), "Remote host disabled")
					return err
				}
				headline := "Remote host enabled"
				if status.TailnetOnly {
					headline = "Remote host enabled on Tailscale"
				} else if status.TailnetListenerEnabled {
					headline = "Additional Tailscale listener enabled"
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\nHost ID: %s\n", headline, status.HostID); err != nil {
					return err
				}
				publicAddress, tailscaleAddress := false, false
				for _, endpoint := range status.Endpoints {
					scheme := "http"
					if endpoint.Secure {
						scheme = "https"
					}
					if endpoint.Kind == "tunnel" {
						publicAddress = true
					}
					if endpoint.Kind == "tailscale" {
						tailscaleAddress = true
					}
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Address: %s://%s\n", scheme, net.JoinHostPort(endpoint.Host, strconv.Itoa(endpoint.Port))); err != nil {
						return err
					}
				}
				if !tailscaleAddress && !status.LoopbackOnly && len(status.Endpoints) > 0 {
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Tailscale: no address; AO Cloud account sync uses only the Tailscale address, so install Tailscale and join your tailnet"); err != nil {
						return err
					}
				}
				if status.TailnetOnly {
					auth := "Authentication: Tailscale identity; callers from your Tailscale user are admitted without a password.\nOther machines on the tailnet list this host automatically (`ao host ls`)."
					if status.PasswordFallback {
						auth += "\nPassword fallback: on (connection password also accepted)"
					}
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), auth); err != nil {
						return err
					}
				}
				if status.TailnetListenerEnabled {
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Tailscale identity authentication; existing LAN/mobile listener remains enabled"); err != nil {
						return err
					}
				}
				if (tunnel || tunnelOnly) && action.name == "enable" {
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Cloudflare terminates TLS and can see the connection password and traffic. Quick-tunnel URLs change on restart and have no uptime guarantee."); err != nil {
						return err
					}
				}
				if !publicAddress && ((tunnel || tunnelOnly) && action.name == "enable" || status.LoopbackOnly || status.Tunnel.Running || status.Tunnel.LastError != "") {
					message := "Tunnel: starting; run `ao remote-host status` for the HTTPS address"
					if !status.Tunnel.Supported {
						flag := "--tunnel"
						if status.LoopbackOnly || tunnelOnly {
							flag = "--tunnel-only"
						}
						message = "Tunnel unavailable: install cloudflared, then rerun `ao remote-host enable " + flag + "`"
					} else if status.Tunnel.LastError != "" && !status.Tunnel.Running {
						message = "Tunnel unavailable: " + status.Tunnel.LastError
					}
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), message); err != nil {
						return err
					}
				}
				if status.Password != "" {
					_, err := fmt.Fprintf(cmd.OutOrStdout(), "Password: %s\n", status.Password)
					return err
				}
				return nil
			},
		}
		if action.name == "enable" {
			cmd.Flags().BoolVar(&lan, "lan", false, "Bind the password-protected listener on every interface (LAN) instead of the Tailscale address only")
			cmd.Flags().BoolVar(&tunnel, "tunnel", false, "Start AO's managed Cloudflare tunnel for access beyond the LAN (implies --lan)")
			cmd.Flags().BoolVar(&tunnelOnly, "tunnel-only", false, "Bind only to loopback and start AO's managed Cloudflare tunnel")
			cmd.Flags().BoolVar(&allowPassword, "allow-password", false, "Also accept the connection password on the Tailscale listener (off by default)")
			cmd.Flags().BoolVar(&alsoTailnet, "also-tailnet", false, "Add identity-only Tailscale discovery (default :3011) while preserving LAN/mobile access")
			cmd.MarkFlagsMutuallyExclusive("tunnel", "tunnel-only", "lan", "also-tailnet")
			cmd.MarkFlagsMutuallyExclusive("allow-password", "lan")
			cmd.MarkFlagsMutuallyExclusive("allow-password", "tunnel")
			cmd.MarkFlagsMutuallyExclusive("allow-password", "tunnel-only")
			cmd.MarkFlagsMutuallyExclusive("allow-password", "also-tailnet")
		}
		root.AddCommand(cmd)
	}
	return root
}

func remoteHostDaemonError(err error) error {
	if errors.Is(err, errDaemonNotRunning) {
		return daemonUnavailableError{message: "AO daemon is not running — run `ao daemon` on this machine", cause: errDaemonNotRunning}
	}
	return err
}
