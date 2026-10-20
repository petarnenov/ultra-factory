package remoteworker

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/agentprofile"
)

func agentStore(workerAgent domain.AgentSelection) *fakeStore {
	profiles := []domain.AgentProfile{
		{ID: "a", Harness: domain.HarnessClaudeCode, Model: "sonnet", Effort: "medium"},
		{ID: "b", Harness: domain.HarnessCodex, Model: "gpt-5", Effort: "high"},
	}
	return &fakeStore{
		sessions: capSessions(),
		project: domain.ProjectRecord{ID: string(capProject), Config: domain.ProjectConfig{
			Coordinators: domain.CoordinatorsConfig{Enabled: true, AgentProfiles: profiles, WorkerAgent: workerAgent},
		}},
	}
}

func TestCheckAgentAsksForHostReadinessBeforePicking(t *testing.T) {
	svc := capService(agentStore(domain.AgentSelection{Mode: domain.AgentSelectionRandom}), nil)
	res, err := svc.CheckAgent(context.Background(), AgentCheck{ParentSessionID: "c1", HostID: "host-build"})
	if err != nil || res.Profile != nil || len(res.NeedReadiness) != 2 || res.NeedReadiness[0] != domain.HarnessClaudeCode {
		t.Fatalf("first pass = %+v, %v", res, err)
	}
	for range 20 {
		res, err = svc.CheckAgent(context.Background(), AgentCheck{
			ParentSessionID: "c1", HostID: "host-build", ReadinessChecked: true,
			Unready: map[string]string{"claude-code": "not_installed"},
		})
		if err != nil || res.Profile == nil || res.Profile.ID != "b" || len(res.NeedReadiness) != 0 {
			t.Fatalf("second pass = %+v, %v", res, err)
		}
	}
}

func TestCheckAgentRefusesWhenNoSelectedProfileIsReadyOnTheHost(t *testing.T) {
	svc := capService(agentStore(domain.AgentSelection{Mode: domain.AgentSelectionFixed, ProfileID: "a"}), nil)
	_, err := svc.CheckAgent(context.Background(), AgentCheck{
		ParentSessionID: "c1", HostID: "host-build", ReadinessChecked: true,
		Unready: map[string]string{"claude-code": "unauthorized"},
	})
	if codeOf(err) != "WORKER_PROFILE_UNAVAILABLE" {
		t.Fatalf("err = %v, want WORKER_PROFILE_UNAVAILABLE", err)
	}
}

func TestCheckAgentRefusesAFlagOutsideTheSelectionWithoutAskingTheHost(t *testing.T) {
	svc := capService(agentStore(domain.AgentSelection{Mode: domain.AgentSelectionFixed, ProfileID: "a"}), nil)
	_, err := svc.CheckAgent(context.Background(), AgentCheck{
		ParentSessionID: "c1", HostID: "host-build", Explicit: agentprofile.Explicit{Harness: "aider"},
	})
	if codeOf(err) != "WORKER_PROFILE_NOT_ALLOWED" {
		t.Fatalf("err = %v, want WORKER_PROFILE_NOT_ALLOWED", err)
	}
}

func TestCheckAgentLeavesInheritAndRootFlagsAlone(t *testing.T) {
	svc := capService(agentStore(domain.AgentSelection{}), nil)
	res, err := svc.CheckAgent(context.Background(), AgentCheck{ParentSessionID: "c1", HostID: "host-build"})
	if err != nil || res.Profile != nil || len(res.NeedReadiness) != 0 {
		t.Fatalf("inherit = %+v, %v", res, err)
	}
	store := agentStore(domain.AgentSelection{})
	cfg := store.project.Config
	cfg.Coordinators.RootWorkerAgent = domain.AgentSelection{Mode: domain.AgentSelectionFixed, ProfileID: "a"}
	store.project.Config = cfg
	res, err = capService(store, nil).CheckAgent(context.Background(), AgentCheck{
		ParentSessionID: "ao-1", HostID: "host-build", Explicit: agentprofile.Explicit{Harness: "aider"},
	})
	if err != nil || res.Profile != nil || len(res.NeedReadiness) != 0 {
		t.Fatalf("explicit flags win for the root: %+v, %v", res, err)
	}
}
