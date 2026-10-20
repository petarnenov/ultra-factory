package daemon

import (
	"log/slog"

	"github.com/aoagents/agent-orchestrator/backend/internal/observe/remotereports"
	coordownership "github.com/aoagents/agent-orchestrator/backend/internal/service/ownership"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/remoteworker"
)

// remoteReportsStore is what both halves of remote report pulling persist to.
type remoteReportsStore interface {
	remotereports.Store
	remoteworker.Store
}

// newRemoteReports wires report pulling for workers spawned with
// `ao spawn --host`: they report into their own host's outbox, where no
// orchestrator for the project runs. The observer pulls from saved hosts
// (outbound only) and the link service records which workers to pull, waking
// the observer as soon as a link is stored. Both refuse this machine's own
// host ID.
func newRemoteReports(store remoteReportsStore, deliverer remotereports.Deliverer, selfHostID string, capacity *coordownership.Guard, log *slog.Logger) (*remotereports.Observer, *remoteworker.Service) {
	observer := remotereports.New(remotereports.Deps{
		Store: store, Deliverer: deliverer, SelfHostID: selfHostID, Logger: log,
	})
	links := remoteworker.New(remoteworker.Deps{
		Store: store, SelfHostID: selfHostID, OnLinked: observer.Wake, Capacity: capacity,
	})
	return observer, links
}
