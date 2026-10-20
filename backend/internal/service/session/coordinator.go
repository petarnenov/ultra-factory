package session

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	coordownership "github.com/aoagents/agent-orchestrator/backend/internal/service/ownership"
)

// defaultCoordinatorPrompt is the brief when the root sends none; a stored
// prompt keeps a relaunch resumable.
const defaultCoordinatorPrompt = "Coordinate the scope in your system prompt. " +
	"Delegate to workers you spawn and report to the project root."

// countCapacity reads the project's census, including remote links when the
// store lists them.
func (s *Service) countCapacity(ctx context.Context, project domain.ProjectID) (coordownership.Counts, error) {
	links, _ := s.store.(coordownership.LinkLister)
	return coordownership.Count(ctx, s.store, links, project)
}

// spawnOwner derives a new worker's durable owner from its spawn parent.
func (s *Service) spawnOwner(ctx context.Context, cfg ports.SpawnConfig) (*domain.SessionID, error) {
	if cfg.Kind != domain.KindWorker || cfg.ParentSessionID == "" || s.store == nil {
		return nil, nil
	}
	parent, ok, err := s.store.GetSession(ctx, cfg.ParentSessionID)
	if err != nil {
		return nil, fmt.Errorf("load spawn parent %s: %w", cfg.ParentSessionID, err)
	}
	if !ok {
		return nil, nil
	}
	return coordownership.SpawnOwner(parent, cfg.ProjectID), nil
}

// admitWorker counts under the project lock; hold the release until the row
// exists. The config is read under the exclusive lock.
func (s *Service) admitWorker(ctx context.Context, project domain.ProjectID, owner *domain.SessionID) (func(), error) {
	mu := s.projectLock(project)
	mu.RLock()
	counts, err := s.countCapacity(ctx, project)
	if err != nil {
		mu.RUnlock()
		return nil, err
	}
	if counts.Coordinators == 0 && owner == nil {
		return mu.RUnlock, nil
	}
	mu.RUnlock()
	mu.Lock()
	limits, counts, err := s.limitsAndCounts(ctx, project)
	if err == nil {
		err = counts.CheckNewWorker(project, owner, limits)
	}
	if err != nil {
		mu.Unlock()
		return nil, err
	}
	return mu.Unlock, nil
}

// limitsAndCounts reads the project's limits and census; the caller holds
// the project lock exclusively.
func (s *Service) limitsAndCounts(ctx context.Context, project domain.ProjectID) (domain.CoordinatorLimits, coordownership.Counts, error) {
	cfg, err := coordownership.LoadConfig(ctx, s.store, project)
	if err != nil {
		return domain.CoordinatorLimits{}, coordownership.Counts{}, err
	}
	counts, err := s.countCapacity(ctx, project)
	return cfg.Limits(), counts, err
}

// requireRoot loads the caller and accepts only the project's live root.
func (s *Service) requireRoot(ctx context.Context, cfg ports.SpawnConfig) error {
	caller, found, err := s.store.GetSession(ctx, cfg.ParentSessionID)
	if err != nil {
		return fmt.Errorf("load spawn caller %s: %w", cfg.ParentSessionID, err)
	}
	return coordownership.RequireRootCaller(caller, found, cfg.ParentSessionID, cfg.ProjectID)
}

// spawnCoordinator checks root, scope, then the gate; the root and config are
// re-read under the project lock.
func (s *Service) spawnCoordinator(ctx context.Context, cfg ports.SpawnConfig) (domain.Session, int, int, error) {
	if s.store == nil {
		return domain.Session{}, 0, 0, fmt.Errorf("spawn coordinator: store is required")
	}
	if err := s.requireRoot(ctx, cfg); err != nil {
		return domain.Session{}, 0, 0, err
	}
	scope, err := coordownership.NormalizeScope(cfg.CoordinatorScope)
	if err != nil {
		return domain.Session{}, 0, 0, err
	}
	cfg.CoordinatorScope = scope
	cfg.Async = true
	if cfg.Prompt == "" {
		cfg.Prompt = defaultCoordinatorPrompt
	}
	unlock := s.lockOrchestratorProject(cfg.ProjectID)
	defer unlock()
	if err := s.requireRoot(ctx, cfg); err != nil {
		return domain.Session{}, 0, 0, err
	}
	gate, err := coordownership.LoadConfig(ctx, s.store, cfg.ProjectID)
	if err != nil {
		return domain.Session{}, 0, 0, err
	}
	if err := coordownership.RequireEnabled(gate, cfg.ProjectID); err != nil {
		return domain.Session{}, 0, 0, err
	}
	limits := gate.Limits()
	if cfg.CoordinatorWorkerLimit != nil {
		if got := *cfg.CoordinatorWorkerLimit; got < 1 || got > limits.WorkersPerCoordinator {
			return domain.Session{}, 0, 0, coordownership.WorkerLimitInvalidError(got, limits.WorkersPerCoordinator)
		}
	}
	counts, err := s.countCapacity(ctx, cfg.ProjectID)
	if err != nil {
		return domain.Session{}, 0, 0, err
	}
	if err := counts.CheckCoordinator(cfg.ProjectID, limits); err != nil {
		return domain.Session{}, 0, 0, err
	}
	if cfg, err = s.resolveCoordinatorAgent(ctx, cfg, gate); err != nil {
		return domain.Session{}, 0, 0, err
	}
	return s.spawn(ctx, cfg)
}

// admitRestore re-checks capacity before a terminated session returns to
// work; hold the release across the restore.
func (s *Service) admitRestore(ctx context.Context, rec domain.SessionRecord) (func(), error) {
	switch rec.Kind {
	case domain.KindCoordinator:
		unlock := s.lockOrchestratorProject(rec.ProjectID)
		gate, err := coordownership.LoadConfig(ctx, s.store, rec.ProjectID)
		var counts coordownership.Counts
		if err == nil {
			counts, err = s.countCapacity(ctx, rec.ProjectID)
		}
		if err == nil {
			err = counts.CheckCoordinator(rec.ProjectID, gate.Limits())
		}
		if err != nil {
			unlock()
			return nil, err
		}
		return unlock, nil
	case domain.KindWorker:
		return s.admitWorker(ctx, rec.ProjectID, rec.OwnerSessionID)
	}
	return func() {}, nil
}
