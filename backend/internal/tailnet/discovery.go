package tailnet

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
)

// Host is an AO daemon found on the tailnet. URL is always the Tailscale
// address, never a LAN address.
type Host struct {
	HostID  string
	Label   string
	Name    string
	DNSName string
	Address string
	Port    int
	URL     string
	Online  bool
	Hidden  bool
	// LastSeen is when the identity probe last answered.
	LastSeen time.Time
}

// Snapshot is the latest discovery result.
type Snapshot struct {
	// Available is false when tailscaled could not be read; Error says why.
	Available bool
	Error     string
	SelfLogin string
	// LocalHostIDs are this install and its siblings, never listed as remotes.
	LocalHostIDs []string
	Hosts        []Host
	UpdatedAt    time.Time
}

// Prober answers the host ID of the AO daemon at base, or an error.
type Prober func(ctx context.Context, base string) (string, error)

// DefaultProber probes GET /api/v1/identity with a direct client.
func DefaultProber(timeout time.Duration) Prober {
	client := remotehost.DirectClient(nil, timeout)
	return func(ctx context.Context, base string) (string, error) {
		return remotehost.ReadIdentity(ctx, client, base)
	}
}

// DiscoveryDeps configures a Discovery.
type DiscoveryDeps struct {
	Run   Runner
	Probe Prober
	// SelfHostID and SiblingHostIDs are never listed; SiblingHostIDs is read
	// on every refresh so a sibling install started later is still excluded.
	SelfHostID     string
	SiblingHostIDs func() []string
	// Ports are probed in order on every online peer; the first that answers
	// with an AO identity wins.
	Ports    []int
	Interval time.Duration
	// ProbeTimeout bounds one identity probe. Peers are probed concurrently.
	ProbeTimeout time.Duration
	PrefsPath    string
	Log          *slog.Logger
	Now          func() time.Time
}

// Discovery keeps the tailnet host list fresh.
type Discovery struct {
	deps DiscoveryDeps

	mu    sync.Mutex
	snap  Snapshot
	known map[string]Host // by peer StableID; keeps an offline host listed
	wake  chan struct{}
}

// NewDiscovery builds a Discovery; call Run to keep it refreshing.
func NewDiscovery(d DiscoveryDeps) *Discovery {
	if d.Run == nil {
		d.Run = ExecRunner
	}
	if d.ProbeTimeout <= 0 {
		d.ProbeTimeout = 2 * time.Second
	}
	if d.Probe == nil {
		d.Probe = DefaultProber(d.ProbeTimeout)
	}
	if len(d.Ports) == 0 {
		d.Ports = []int{DefaultPort}
	}
	if d.Interval <= 0 {
		d.Interval = 30 * time.Second
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.SiblingHostIDs == nil {
		d.SiblingHostIDs = func() []string { return nil }
	}
	return &Discovery{deps: d, known: map[string]Host{}, wake: make(chan struct{}, 1)}
}

// Run refreshes on Interval until ctx ends. Wake forces an early refresh.
func (d *Discovery) Run(ctx context.Context) {
	d.Refresh(ctx)
	ticker := time.NewTicker(d.deps.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.Refresh(ctx)
		case <-d.wake:
			d.Refresh(ctx)
		}
	}
}

// Wake asks Run to refresh now.
func (d *Discovery) Wake() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// Snapshot returns the latest result without touching tailscaled.
func (d *Discovery) Snapshot() Snapshot {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.snap
}

// Find returns the discovered host recorded under hostID, hidden or not.
func (d *Discovery) Find(hostID string) (Host, bool) {
	for _, host := range d.Snapshot().Hosts {
		if host.HostID == hostID {
			return host, true
		}
	}
	return Host{}, false
}

// Refresh reads the peer list, probes every online peer, and publishes the
// new snapshot, which it also returns.
func (d *Discovery) Refresh(ctx context.Context) Snapshot {
	local := append([]string{d.deps.SelfHostID}, d.deps.SiblingHostIDs()...)
	excluded := map[string]bool{}
	for _, id := range local {
		if id != "" {
			excluded[id] = true
		}
	}
	now := d.deps.Now()
	st, err := ReadStatus(ctx, d.deps.Run)
	next := Snapshot{LocalHostIDs: local, UpdatedAt: now}
	if err != nil {
		d.mu.Lock()
		defer d.mu.Unlock()
		next.Error = err.Error()
		for id, host := range d.known {
			host.Online = false
			d.known[id] = host
		}
		next.Hosts = d.listLocked(excluded)
		d.snap = next
		return next
	}
	next.Available = true
	next.SelfLogin = st.SelfLogin
	// Probe without the lock so readers see the previous snapshot meanwhile.
	current := map[string]bool{}
	var wg sync.WaitGroup
	var found sync.Map
	for _, peer := range st.Peers {
		if peer.StableID == "" || peer.StableID == st.Self.StableID || peer.Address == "" {
			continue
		}
		current[peer.StableID] = true
		if !peer.Online {
			continue
		}
		wg.Add(1)
		go func(peer Node) {
			defer wg.Done()
			if host, ok := d.probePeer(ctx, peer, now); ok {
				found.Store(peer.StableID, host)
			}
		}(peer)
	}
	wg.Wait()
	d.mu.Lock()
	defer d.mu.Unlock()
	for id, host := range d.known {
		if !current[id] {
			delete(d.known, id)
			continue
		}
		host.Online = false
		d.known[id] = host
	}
	found.Range(func(key, value any) bool {
		id, _ := key.(string)
		host, _ := value.(Host)
		d.known[id] = host
		return true
	})
	next.Hosts = d.listLocked(excluded)
	d.snap = next
	return next
}

// probePeer finds the AO daemon on peer by trying each port in order.
func (d *Discovery) probePeer(ctx context.Context, peer Node, now time.Time) (Host, bool) {
	for _, port := range d.deps.Ports {
		base := "http://" + net.JoinHostPort(peer.Address, strconv.Itoa(port))
		probeCtx, cancel := context.WithTimeout(ctx, d.deps.ProbeTimeout)
		hostID, err := d.deps.Probe(probeCtx, base)
		cancel()
		if err != nil {
			continue
		}
		return Host{
			HostID: hostID, Name: peer.Name, DNSName: peer.DNSName, Address: peer.Address,
			Port: port, URL: base, Online: true, LastSeen: now,
		}, true
	}
	return Host{}, false
}

// listLocked renders known hosts with prefs applied, excluding local installs.
func (d *Discovery) listLocked(excluded map[string]bool) []Host {
	prefs, err := LoadPrefs(d.deps.PrefsPath)
	if err != nil {
		d.deps.Log.Warn("tailnet: read host prefs failed", "err", err)
	}
	seen := map[string]bool{}
	var hosts []Host
	for _, host := range d.known {
		if excluded[host.HostID] || seen[host.HostID] {
			continue
		}
		seen[host.HostID] = true
		host.Hidden = prefs.hidden(host.HostID)
		host.Label = host.Name
		if label, ok := prefs.Labels[host.HostID]; ok && label != "" {
			host.Label = label
		}
		hosts = append(hosts, host)
	}
	sort.Slice(hosts, func(i, j int) bool {
		if hosts[i].Name != hosts[j].Name {
			return hosts[i].Name < hosts[j].Name
		}
		return hosts[i].HostID < hosts[j].HostID
	})
	return hosts
}

// SetHidden hides or shows hostID in every list that reads this daemon.
func (d *Discovery) SetHidden(hostID string, hidden bool) error {
	return d.updatePrefs(func(p *Prefs) { p.setHidden(hostID, hidden) })
}

// SetLabel overrides the label shown for hostID; "" restores the machine name.
func (d *Discovery) SetLabel(hostID, label string) error {
	return d.updatePrefs(func(p *Prefs) { p.setLabel(hostID, label) })
}

func (d *Discovery) updatePrefs(apply func(*Prefs)) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.deps.PrefsPath == "" {
		return fmt.Errorf("tailnet host prefs are not configured")
	}
	prefs, err := LoadPrefs(d.deps.PrefsPath)
	if err != nil {
		return err
	}
	apply(&prefs)
	if err := SavePrefs(d.deps.PrefsPath, prefs); err != nil {
		return err
	}
	excluded := map[string]bool{}
	for _, id := range d.snap.LocalHostIDs {
		excluded[id] = true
	}
	d.snap.Hosts = d.listLocked(excluded)
	return nil
}
