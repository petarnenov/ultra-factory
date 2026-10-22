package daemon

import (
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/observe/remotereports"
	coordownership "github.com/aoagents/agent-orchestrator/backend/internal/service/ownership"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/remoteworker"
)

// remoteReportsStore is what both halves of remote report pulling persist to.
type remoteReportsStore interface {
	remotereports.Store
	remoteworker.Store
}

// newRemoteReports wires remote worker report pulling and link discovery.
// The link service wakes the observer as soon as a link is stored.
func newRemoteReports(store remoteReportsStore, deliverer remotereports.Deliverer, selfHostID string, capacity *coordownership.Guard, notifier coordownership.Notifier, log *slog.Logger) (*remotereports.Observer, *remoteworker.Service) {
	var observer *remotereports.Observer
	links := remoteworker.New(remoteworker.Deps{
		Store: store, SelfHostID: selfHostID, OnLinked: func() { observer.Wake() }, Capacity: capacity,
		Notifier: notifier, Logger: log,
	})
	observer = remotereports.New(remotereports.Deps{
		Store: store, Deliverer: deliverer, SelfHostID: selfHostID, Logger: log,
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
