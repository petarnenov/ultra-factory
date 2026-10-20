package report

import (
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestSummaryRenderNamesReturnedRemoteWorkers(t *testing.T) {
	out := summaryBatch{Summaries: []domain.CoordinatorSummary{{
		ID: "s1", CoordinatorSessionID: "coord", ProjectID: "p", Scope: "auth", State: domain.SummaryBlocked,
		EscalationReason: domain.EscalationCoordinatorTerminated,
		RemoteWorkers:    []domain.SummaryRemoteWorker{{HostID: "host-build", RemoteSessionID: "widgets-3"}},
	}}}.Render()
	if !strings.Contains(out, "remote workers: host-build/widgets-3") {
		t.Fatalf("rendered summary = %q, want the remote worker named", out)
	}
}
