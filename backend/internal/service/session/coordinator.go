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
// exists. Without coordinators it is shared, otherwise exclusive.
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
	counts, err = s.countCapacity(ctx, project)
	if err == nil {
		err = counts.CheckNewWorker(project, owner)
	}
	if err != nil {
		mu.Unlock()
		return nil, err
	}
	return mu.Unlock, nil
}

// spawnCoordinator requires the live root and shares the project lock with
// root spawn across its check and insert.
func (s *Service) spawnCoordinator(ctx context.Context, cfg ports.SpawnConfig) (domain.Session, int, int, error) {
	if s.store == nil {
		return domain.Session{}, 0, 0, fmt.Errorf("spawn coordinator: store is required")
	}
	caller, found, err := s.store.GetSession(ctx, cfg.ParentSessionID)
	if err != nil {
		return domain.Session{}, 0, 0, fmt.Errorf("load spawn caller %s: %w", cfg.ParentSessionID, err)
	}
	if err := coordownership.RequireRootCaller(caller, found, cfg.ParentSessionID, cfg.ProjectID); err != nil {
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
	counts, err := s.countCapacity(ctx, cfg.ProjectID)
	if err != nil {
		return domain.Session{}, 0, 0, err
	}
	if counts.Coordinators >= coordownership.MaxCoordinators {
		return domain.Session{}, 0, 0, coordownership.CoordinatorLimitError(cfg.ProjectID, counts.Coordinators)
	}
	return s.spawn(ctx, cfg)
}

// admitRestore re-checks capacity before a terminated session returns to
// work; hold the release across the restore.
func (s *Service) admitRestore(ctx context.Context, rec domain.SessionRecord) (func(), error) {
	switch rec.Kind {
	case domain.KindCoordinator:
		unlock := s.lockOrchestratorProject(rec.ProjectID)
		counts, err := s.countCapacity(ctx, rec.ProjectID)
		if err == nil && counts.Coordinators >= coordownership.MaxCoordinators {
			err = coordownership.CoordinatorLimitError(rec.ProjectID, counts.Coordinators)
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
