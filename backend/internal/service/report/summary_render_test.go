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
		RemoteWorkers:    []domain.SummaryRemoteWorker{{HostID: "host-build", HostLabel: "build host", RemoteSessionID: "widgets-3"}},
	}}}.Render()
	if !strings.Contains(out, "remote worker: widgets-3 on build host") {
		t.Fatalf("rendered summary = %q, want the host-labeled remote worker", out)
	}
	if got := strings.Count(out, "widgets-3"); got != 1 {
		t.Fatalf("remote worker appears %d times in rendered summary %q, want once", got, out)
	}
}
