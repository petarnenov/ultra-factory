package remoteworker

import (
	"context"
	"errors"
	"sort"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/agentprofile"
	coordownership "github.com/aoagents/agent-orchestrator/backend/internal/service/ownership"
)

// AgentCheck is the agent half of the pre-spawn check for a remote worker.
type AgentCheck struct {
	ParentSessionID domain.SessionID
	HostID          string
	Explicit        agentprofile.Explicit
	// ReadinessChecked says the caller queried the host. Unready then maps
	// each unusable harness to its reason; any other counts as ready.
	ReadinessChecked bool
	Unready          map[string]string
}

// AgentCheckResult is the outcome: a picked profile, or the harnesses whose
// readiness on the host the caller must report before a pick is possible.
type AgentCheckResult struct {
	agentprofile.Result
	NeedReadiness []domain.AgentHarness
}

// CheckAgent applies the parent's agent selection to a worker on another
// host, whose readiness the caller reports. Nothing is stored.
func (s *Service) CheckAgent(ctx context.Context, in AgentCheck) (AgentCheckResult, error) {
	if s == nil || s.store == nil {
		return AgentCheckResult{}, errors.New("remoteworker: store is required")
	}
	parent, err := s.linkParent(ctx, in.ParentSessionID)
	if err != nil {
		return AgentCheckResult{}, err
	}
	gate, err := coordownership.LoadConfig(ctx, s.store, parent.ProjectID)
	if err != nil {
		return AgentCheckResult{}, err
	}
	req, applies := agentprofile.ForWorkerSpawn(parent, parent.ProjectID, gate)
	if !applies {
		return AgentCheckResult{}, nil
	}
	req.HostID, req.Explicit = in.HostID, in.Explicit
	needed := map[domain.AgentHarness]bool{}
	req.Ready = func(_ context.Context, h domain.AgentHarness) (bool, string) {
		needed[h] = true
		if reason, bad := in.Unready[string(h)]; in.ReadinessChecked && bad {
			return false, reason
		}
		return true, ""
	}
	res, err := agentprofile.Resolve(ctx, req)
	if err != nil {
		return AgentCheckResult{}, err
	}
	out := AgentCheckResult{Result: res}
	if !in.ReadinessChecked && len(needed) > 0 {
		for h := range needed {
			out.NeedReadiness = append(out.NeedReadiness, h)
		}
		sort.Slice(out.NeedReadiness, func(i, j int) bool { return out.NeedReadiness[i] < out.NeedReadiness[j] })
		out.Result = agentprofile.Result{}
	}
	return out, nil
}
