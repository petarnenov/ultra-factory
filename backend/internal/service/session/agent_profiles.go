package session

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/agentprofile"
	coordownership "github.com/aoagents/agent-orchestrator/backend/internal/service/ownership"
)

// agentSelectionStore is the optional store capability behind a live change
// of a coordinator's worker agent selection.
type agentSelectionStore interface {
	SetCoordinatorWorkerAgent(ctx context.Context, id domain.SessionID, selection domain.AgentSelection, event domain.CoordinatorAgentEvent, at time.Time) (*domain.AgentSelection, error)
}

// localHarnessReady is the readiness filter for placement on this daemon. Only
// a definite negative excludes a harness; unknown readiness does not block a
// launch.
func (s *Service) localHarnessReady(ctx context.Context, harness domain.AgentHarness) (bool, string) {
	if s.agentReadiness == nil {
		return true, ""
	}
	snapshot, err := s.agentReadiness.EnsureAgentReadiness(ctx, string(harness), domain.AgentReadinessPurposeLaunch)
	if err != nil {
		return false, "readiness check failed"
	}
	if snapshot.EffectiveReadiness != domain.AgentReadinessNotReady {
		return true, ""
	}
	if snapshot.Installation.State == domain.AgentInstallationNotInstalled {
		return false, domain.AgentReadinessReasonNotInstalled
	}
	return false, domain.AgentReadinessReasonUnauthorized
}

// explicitAgent extracts the agent flags a caller named on a spawn. A harness
// resolved by the CLI from project settings is not explicit.
func explicitAgent(cfg ports.SpawnConfig) agentprofile.Explicit {
	e := agentprofile.Explicit{Model: cfg.AgentConfig.Model, Effort: cfg.AgentConfig.Effort}
	named := cfg.Harness != ""
	if cfg.HarnessExplicitSet {
		named = cfg.HarnessExplicit
	}
	if named {
		e.Harness = cfg.Harness
	}
	return e
}

// applyProfile makes the chosen profile the spawn's explicit agent.
func applyProfile(cfg ports.SpawnConfig, res agentprofile.Result) ports.SpawnConfig {
	cfg.AgentProfileID = res.RecordID
	if res.Profile == nil {
		return cfg
	}
	cfg.Harness = res.Profile.Harness
	cfg.HarnessExplicit, cfg.HarnessExplicitSet = true, true
	cfg.AgentConfig.Model = res.Profile.Model
	cfg.AgentConfig.Effort = res.Profile.Effort
	cfg.EffortOverride = true
	return cfg
}

// resolveWorkerAgent applies the coordinator's or the root's agent selection
// to a worker spawn. The caller holds the project lock so the selection read
// here is the one in force when the worker is counted.
func (s *Service) resolveWorkerAgent(ctx context.Context, cfg ports.SpawnConfig, owner *domain.SessionID) (ports.SpawnConfig, error) {
	if cfg.ParentSessionID == "" || s.store == nil {
		return cfg, nil
	}
	parent, ok, err := s.store.GetSession(ctx, cfg.ParentSessionID)
	if err != nil {
		return cfg, fmt.Errorf("load spawn parent %s: %w", cfg.ParentSessionID, err)
	}
	if !ok {
		return cfg, nil
	}
	gate, err := coordownership.LoadConfig(ctx, s.store, cfg.ProjectID)
	if err != nil {
		return cfg, err
	}
	req := agentprofile.Request{
		ProjectID: cfg.ProjectID,
		Profiles:  gate.AgentProfiles,
		Explicit:  explicitAgent(cfg),
		Ready:     s.localHarnessReady,
		Intn:      s.profileIntn,
	}
	switch {
	case owner != nil:
		req.Scope, req.Coordinator, req.Selection = agentprofile.ScopeCoordinatorWorker, *owner, gate.WorkerAgent
		if parent.CoordinatorWorkerAgent != nil {
			req.Selection = *parent.CoordinatorWorkerAgent
		}
	case coordownership.IsLiveRoot(parent, cfg.ProjectID):
		req.Scope, req.Selection = agentprofile.ScopeRootWorker, gate.RootWorkerAgent
	default:
		return cfg, nil
	}
	res, err := agentprofile.Resolve(ctx, req)
	if err != nil {
		return cfg, err
	}
	return applyProfile(cfg, res), nil
}

// resolveCoordinatorAgent validates the per-coordinator agent settings of a
// coordinator spawn and picks its own profile. The caller holds the project
// lock and passes the project's coordinator config.
func (s *Service) resolveCoordinatorAgent(ctx context.Context, cfg ports.SpawnConfig, gate domain.CoordinatorsConfig) (ports.SpawnConfig, error) {
	if sel := cfg.CoordinatorWorkerAgent; sel != nil {
		normalized := normalizeSelection(*sel)
		if err := domain.ValidateAgentSelection("workerAgent", normalized, gate.AgentProfiles); err != nil {
			return cfg, selectionError(err)
		}
		cfg.CoordinatorWorkerAgent = &normalized
	}
	selection := gate.CoordinatorAgent
	if sel := cfg.CoordinatorAgent; sel != nil {
		selection = normalizeSelection(*sel)
		if err := domain.ValidateAgentSelection("coordinatorAgent", selection, gate.AgentProfiles); err != nil {
			return cfg, coordinatorSelectionError(err)
		}
	}
	res, err := agentprofile.Resolve(ctx, agentprofile.Request{
		Scope:     agentprofile.ScopeCoordinator,
		ProjectID: cfg.ProjectID,
		Profiles:  gate.AgentProfiles,
		Selection: selection,
		Explicit:  explicitAgent(cfg),
		Ready:     s.localHarnessReady,
		Intn:      s.profileIntn,
	})
	if err != nil {
		return cfg, err
	}
	return applyProfile(cfg, res), nil
}

// normalizeSelection fills the default mode and drops fields the mode ignores.
func normalizeSelection(sel domain.AgentSelection) domain.AgentSelection {
	if sel.Mode == "" {
		sel.Mode = domain.AgentSelectionInherit
	}
	if sel.Mode == domain.AgentSelectionInherit {
		return domain.AgentSelection{Mode: domain.AgentSelectionInherit}
	}
	sel.ProfileID = strings.TrimSpace(sel.ProfileID)
	return sel
}

// selectionError maps a selection check to WORKER_PROFILE_INVALID.
func selectionError(err error) error {
	if perr, ok := err.(*domain.AgentProfileError); ok {
		return apierr.Invalid("WORKER_PROFILE_INVALID", perr.Error(), map[string]any{"field": perr.Field})
	}
	return err
}

// coordinatorSelectionError maps an unknown profile in a coordinator agent
// selection to COORDINATOR_PROFILE_NOT_ALLOWED, everything else to
// WORKER_PROFILE_INVALID.
func coordinatorSelectionError(err error) error {
	if perr, ok := err.(*domain.AgentProfileError); ok && perr.UnknownProfileID != "" {
		return apierr.Invalid("COORDINATOR_PROFILE_NOT_ALLOWED", perr.Error(), map[string]any{"profileId": perr.UnknownProfileID})
	}
	return selectionError(err)
}

// CoordinatorAgentCaller names who changes a worker agent selection; the
// rules are those of a live limit change.
type CoordinatorAgentCaller = CoordinatorLimitsCaller

// SetCoordinatorWorkerAgent changes a live coordinator's worker agent
// selection for workers spawned after the change. An unchanged selection is a
// no-op: no audit row and no message.
func (s *Service) SetCoordinatorWorkerAgent(
	ctx context.Context,
	coordinatorID domain.SessionID,
	selection domain.AgentSelection,
	caller CoordinatorAgentCaller,
) (domain.SessionRecord, error) {
	if s.store == nil {
		return domain.SessionRecord{}, fmt.Errorf("set coordinator worker agent %s: store is required", coordinatorID)
	}
	writer, ok := s.store.(agentSelectionStore)
	if !ok {
		return domain.SessionRecord{}, fmt.Errorf("set coordinator worker agent %s: store does not support agent selections", coordinatorID)
	}
	origin, err := limitOrigin(caller)
	if err != nil {
		return domain.SessionRecord{}, err
	}
	selection = normalizeSelection(selection)
	updated, changed, err := s.applyCoordinatorWorkerAgent(ctx, writer, coordinatorID, selection, caller, origin)
	if err != nil || !changed {
		return updated, err
	}
	msg := fmt.Sprintf("Your workers now run on: %s. Running workers keep their agent; the new selection applies to workers you spawn from now on.", describeSelection(selection))
	if s.manager == nil {
		return updated, nil
	}
	if sendErr := s.manager.Send(ctx, coordinatorID, msg, nil); sendErr != nil && s.logger != nil {
		s.logger.Warn("coordinator agent message not delivered", "session", coordinatorID, "error", sendErr)
	}
	return updated, nil
}

func (s *Service) applyCoordinatorWorkerAgent(
	ctx context.Context,
	writer agentSelectionStore,
	coordinatorID domain.SessionID,
	selection domain.AgentSelection,
	caller CoordinatorAgentCaller,
	origin string,
) (domain.SessionRecord, bool, error) {
	project, err := s.limitProject(ctx, coordinatorID, caller)
	if err != nil {
		return domain.SessionRecord{}, false, err
	}
	unlock := s.lockOrchestratorProject(project)
	defer unlock()
	actor, err := s.limitActor(ctx, project, caller)
	if err != nil {
		return domain.SessionRecord{}, false, err
	}
	coordinator, err := s.loadLiveCoordinator(ctx, coordinatorID)
	if err != nil {
		return domain.SessionRecord{}, false, err
	}
	gate, err := coordownership.LoadConfig(ctx, s.store, coordinator.ProjectID)
	if err != nil {
		return domain.SessionRecord{}, false, err
	}
	if err := domain.ValidateAgentSelection("workerAgent", selection, gate.AgentProfiles); err != nil {
		return domain.SessionRecord{}, false, selectionError(err)
	}
	if stored := coordinator.CoordinatorWorkerAgent; stored != nil && reflect.DeepEqual(normalizeSelection(*stored), selection) {
		return coordinator, false, nil
	}
	event := domain.CoordinatorAgentEvent{ActorSessionID: actor, Origin: origin}
	if _, err := writer.SetCoordinatorWorkerAgent(ctx, coordinator.ID, selection, event, s.now()); err != nil {
		return domain.SessionRecord{}, false, err
	}
	updated, _, err := s.store.GetSession(ctx, coordinator.ID)
	return updated, true, err
}

func describeSelection(sel domain.AgentSelection) string {
	switch sel.Mode {
	case domain.AgentSelectionFixed:
		return "profile " + sel.ProfileID
	case domain.AgentSelectionRandom:
		if len(sel.ProfileIDs) == 0 {
			return "a random choice among all project profiles"
		}
		return "a random choice among profiles " + strings.Join(sel.ProfileIDs, ", ")
	}
	return "the project default agent (inherit)"
}
