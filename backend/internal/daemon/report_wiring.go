package daemon

import (
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

// newReportService builds the daemon's report service. The store also dedupes
// PR outcome reports, which the SCM observer files through it.
func newReportService(store *sqlite.Store, artifacts reportsvc.ArtifactRecorder, onCreated func(domain.ReportRecord)) *reportsvc.Service {
	return reportsvc.New(reportsvc.Deps{Store: store, Claims: store, Artifacts: artifacts, OnCreated: onCreated})
}
