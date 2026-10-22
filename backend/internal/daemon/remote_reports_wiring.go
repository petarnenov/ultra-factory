package daemon

import (
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/observe/remotereports"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
	coordownership "github.com/aoagents/agent-orchestrator/backend/internal/service/ownership"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/remoteworker"
	"github.com/aoagents/agent-orchestrator/backend/internal/tailnet"
)

// tailnetHostFinder is the discovery lookup the host resolver consults first.
type tailnetHostFinder interface {
	Find(hostID string) (tailnet.Host, bool)
}

// remoteHostLookup resolves a host ID to a Tailscale-discovered host first
// (password-free, tailnet address), then to the saved-host file.
func remoteHostLookup(discovery tailnetHostFinder) func(string) (remotehost.Entry, bool, error) {
	return func(hostID string) (remotehost.Entry, bool, error) {
		if discovery != nil {
			if host, ok := discovery.Find(hostID); ok && !host.Hidden {
				return remotehost.Entry{Label: host.Label, URL: host.URL, HostID: host.HostID, Tailscale: true}, true, nil
			}
		}
		return remotehost.Find(hostID)
	}
}

// remoteReportsStore is what both halves of remote report pulling persist to.
type remoteReportsStore interface {
	remotereports.Store
	remoteworker.Store
}

// newRemoteReports wires remote worker report pulling and link discovery.
// The link service wakes the observer as soon as a link is stored.
func newRemoteReports(store remoteReportsStore, deliverer remotereports.Deliverer, selfHostID string, capacity *coordownership.Guard, notifier coordownership.Notifier, discovery tailnetHostFinder, log *slog.Logger) (*remotereports.Observer, *remoteworker.Service) {
	lookup := remoteHostLookup(discovery)
	var observer *remotereports.Observer
	links := remoteworker.New(remoteworker.Deps{
		Store: store, SelfHostID: selfHostID, Lookup: lookup, OnLinked: func() { observer.Wake() }, Capacity: capacity,
		Notifier: notifier, Logger: log,
	})
	observer = remotereports.New(remotereports.Deps{
		Store: store, Deliverer: deliverer, SelfHostID: selfHostID, Lookup: lookup, Logger: log,
		Linker: links, DiscoveryInterval: discoveryInterval(log),
	})
	return observer, links
}

// discoveryInterval reads AO_REMOTE_DISCOVERY_INTERVAL, a duration that tests
// shorten. An invalid value is logged and the default is used.
func discoveryInterval(log *slog.Logger) time.Duration {
	raw := strings.TrimSpace(os.Getenv("AO_REMOTE_DISCOVERY_INTERVAL"))
	if raw == "" {
		return remotereports.DefaultDiscoveryInterval
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		log.Warn("remote discovery: ignoring invalid AO_REMOTE_DISCOVERY_INTERVAL", "value", raw)
		return remotereports.DefaultDiscoveryInterval
	}
	return d
}
