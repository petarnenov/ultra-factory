// Package tailnet discovers AO hosts on the user's Tailscale network and
// authenticates tailnet callers through the local tailscaled.
package tailnet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	aoprocess "github.com/aoagents/agent-orchestrator/backend/internal/process"
)

// DefaultPort is the well-known port AO daemons listen on for tailnet callers.
// Discovery probes it on every online peer; AO_TAILNET_PROBE_PORTS adds more.
const DefaultPort = 3011

// commandTimeout bounds every tailscale CLI call: a wedged tailscaled must
// fail a request closed, never hang it.
const commandTimeout = 3 * time.Second

// ErrUnavailable means the local tailscaled could not answer: not installed,
// not running, logged out, or malformed output. Callers fail closed on it.
var ErrUnavailable = errors.New("tailscale is unavailable")

// Runner runs the tailscale CLI and returns its stdout. Injected so tests
// never invoke the real binary.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

// macAppCLI is where the Tailscale macOS app keeps its CLI; it is not on PATH.
const macAppCLI = "/Applications/Tailscale.app/Contents/MacOS/Tailscale"

// tailscaleBinary resolves the CLI: PATH first, then the macOS app bundle.
func tailscaleBinary(lookPath func(string) (string, error), goos string, exists func(string) bool) string {
	if path, err := lookPath("tailscale"); err == nil {
		return path
	}
	if goos == "darwin" && exists(macAppCLI) {
		return macAppCLI
	}
	return "tailscale"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ExecRunner is the production Runner.
func ExecRunner(ctx context.Context, args ...string) ([]byte, error) {
	binary := tailscaleBinary(exec.LookPath, runtime.GOOS, fileExists)
	return aoprocess.CommandContext(ctx, binary, args...).Output()
}

// Node is one machine on the tailnet as tailscaled reports it.
type Node struct {
	StableID string
	// Name is the machine name (the MagicDNS label), the default host label.
	Name string
	// DNSName is the MagicDNS name without the trailing dot.
	DNSName string
	// Address is the node's Tailscale IPv4 address.
	Address string
	Online  bool
	UserID  int64
	Tags    []string
}

// Status is the tailnet as seen from this machine.
type Status struct {
	Self Node
	// SelfLogin is the Tailscale login of this machine's user, the identity
	// every untagged caller is compared against by default.
	SelfLogin      string
	Peers          []Node
	MagicDNSSuffix string
}

type statusNode struct {
	ID          string   `json:"ID"`
	HostName    string   `json:"HostName"`
	DNSName     string   `json:"DNSName"`
	TailscaleIP []string `json:"TailscaleIPs"`
	Online      bool     `json:"Online"`
	UserID      int64    `json:"UserID"`
	Tags        []string `json:"Tags"`
}

func (n statusNode) node() Node {
	dns := strings.TrimSuffix(n.DNSName, ".")
	name := n.HostName
	if label, _, ok := strings.Cut(dns, "."); ok && label != "" {
		name = label
	}
	return Node{StableID: n.ID, Name: name, DNSName: dns, Address: firstIPv4(n.TailscaleIP), Online: n.Online, UserID: n.UserID, Tags: n.Tags}
}

func firstIPv4(addrs []string) string {
	for _, raw := range addrs {
		if ip := net.ParseIP(strings.TrimSpace(raw)); ip != nil && ip.To4() != nil {
			return ip.String()
		}
	}
	return ""
}

// ReadStatus reads this node, its peers, and the user directory from
// `tailscale status --json`. A backend that is not running is ErrUnavailable.
func ReadStatus(ctx context.Context, run Runner) (Status, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	out, err := run(ctx, "status", "--json")
	if err != nil {
		return Status{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	var parsed struct {
		BackendState   string                `json:"BackendState"`
		Self           *statusNode           `json:"Self"`
		Peer           map[string]statusNode `json:"Peer"`
		User           map[string]struct{ LoginName string }
		CurrentTailnet *struct{ MagicDNSSuffix string } `json:"CurrentTailnet"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil || parsed.Self == nil {
		return Status{}, fmt.Errorf("%w: unexpected status output", ErrUnavailable)
	}
	if parsed.BackendState != "" && parsed.BackendState != "Running" {
		return Status{}, fmt.Errorf("%w: backend state %s", ErrUnavailable, parsed.BackendState)
	}
	st := Status{Self: parsed.Self.node()}
	if user, ok := parsed.User[fmt.Sprint(parsed.Self.UserID)]; ok {
		st.SelfLogin = user.LoginName
	}
	if parsed.CurrentTailnet != nil {
		st.MagicDNSSuffix = parsed.CurrentTailnet.MagicDNSSuffix
	}
	for _, peer := range parsed.Peer {
		st.Peers = append(st.Peers, peer.node())
	}
	sort.Slice(st.Peers, func(i, j int) bool { return st.Peers[i].Name < st.Peers[j].Name })
	return st, nil
}

// Identity is what tailscaled knows about the node behind a source address.
type Identity struct {
	NodeName  string
	LoginName string
	UserID    int64
	// Tags is non-empty for a tagged device, which has no user of its own.
	Tags []string
}

// WhoIs resolves the tailnet identity behind addr via `tailscale whois`. Any
// failure, including an address tailscaled does not know, is an error.
func WhoIs(ctx context.Context, run Runner, addr string) (Identity, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	out, err := run(ctx, "whois", "--json", addr)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: whois %s: %w", ErrUnavailable, addr, err)
	}
	var parsed struct {
		Node *struct {
			Name         string   `json:"Name"`
			ComputedName string   `json:"ComputedName"`
			User         int64    `json:"User"`
			Tags         []string `json:"Tags"`
		} `json:"Node"`
		UserProfile *struct {
			ID        int64  `json:"ID"`
			LoginName string `json:"LoginName"`
		} `json:"UserProfile"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil || parsed.Node == nil {
		return Identity{}, fmt.Errorf("%w: unexpected whois output for %s", ErrUnavailable, addr)
	}
	id := Identity{NodeName: parsed.Node.ComputedName, UserID: parsed.Node.User, Tags: parsed.Node.Tags}
	if id.NodeName == "" {
		id.NodeName = strings.TrimSuffix(parsed.Node.Name, ".")
	}
	if parsed.UserProfile != nil {
		id.LoginName = parsed.UserProfile.LoginName
		if id.UserID == 0 {
			id.UserID = parsed.UserProfile.ID
		}
	}
	return id, nil
}
