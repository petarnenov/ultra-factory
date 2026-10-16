package agent

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters"
	agentregistry "github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/registry"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type switchReadinessAgent struct {
	*readinessTestAgent
	mode ports.FreshNativeSessionIDMode
}

func (a *switchReadinessAgent) ContinuationCapabilities() ports.ContinuationCapabilities {
	return ports.ContinuationCapabilities{FreshNativeSessionID: a.mode}
}

type switchReadinessIDAgent struct{ *switchReadinessAgent }

func (a *switchReadinessIDAgent) NewNativeSessionID() string { return "native-id" }

func TestReadinessAdvertisesOnlySupportedSwitchModes(t *testing.T) {
	t.Parallel()

	providerAssigned := &switchReadinessAgent{
		readinessTestAgent: &readinessTestAgent{},
		mode:               ports.FreshNativeSessionIDProviderAssigned,
	}
	callerAssigned := &switchReadinessIDAgent{switchReadinessAgent: &switchReadinessAgent{
		readinessTestAgent: &readinessTestAgent{},
		mode:               ports.FreshNativeSessionIDCallerAssigned,
	}}
	withoutCapabilities := &readinessTestAgent{}
	items := []agentregistry.HarnessAgent{
		{Harness: "provider", Manifest: adapters.Manifest{ID: "provider", Name: "Provider"}, Agent: providerAssigned},
		{Harness: "caller", Manifest: adapters.Manifest{ID: "caller", Name: "Caller"}, Agent: callerAssigned},
		{Harness: "unsupported", Manifest: adapters.Manifest{ID: "unsupported", Name: "Unsupported"}, Agent: withoutCapabilities},
	}
	svc := &Service{
		agents: items,
		chatSupport: func(id domain.AgentHarness) bool {
			return id == "provider"
		},
	}
	input := []domain.AgentReadinessSnapshot{
		{ID: "provider", Installation: domain.AgentInstallationObservation{State: domain.AgentInstallationInstalled}},
		{ID: "caller", Installation: domain.AgentInstallationObservation{State: domain.AgentInstallationInstalled}},
		{ID: "unsupported", Installation: domain.AgentInstallationObservation{State: domain.AgentInstallationInstalled}},
	}

	got, err := svc.withReadinessUsage(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]domain.SessionMode{
		"provider": {domain.SessionModeTUI, domain.SessionModeChat},
		"caller":   {domain.SessionModeTUI},
	}
	for _, snapshot := range got.Agents {
		modes, ok := want[snapshot.ID]
		if !ok {
			if len(snapshot.SwitchModes) != 0 {
				t.Errorf("%s switch modes = %v, want no supported modes", snapshot.ID, snapshot.SwitchModes)
			}
			continue
		}
		if len(snapshot.SwitchModes) != len(modes) {
			t.Errorf("%s switch modes = %v, want %v", snapshot.ID, snapshot.SwitchModes, modes)
			continue
		}
		for i := range modes {
			if snapshot.SwitchModes[i] != modes[i] {
				t.Errorf("%s switch modes = %v, want %v", snapshot.ID, snapshot.SwitchModes, modes)
				break
			}
		}
	}
}

func TestCallerAssignedSwitchModeRequiresIDProvider(t *testing.T) {
	withoutIDProvider := &switchReadinessAgent{
		readinessTestAgent: &readinessTestAgent{},
		mode:               ports.FreshNativeSessionIDCallerAssigned,
	}
	if agentCanReceiveSwitch(withoutIDProvider) {
		t.Fatal("caller-assigned harness without an ID provider was switch-capable")
	}
	withIDProvider := &switchReadinessIDAgent{switchReadinessAgent: &switchReadinessAgent{
		readinessTestAgent: &readinessTestAgent{},
		mode:               ports.FreshNativeSessionIDCallerAssigned,
	}}
	if !agentCanReceiveSwitch(withIDProvider) {
		t.Fatal("caller-assigned harness with an ID provider was not switch-capable")
	}
}
