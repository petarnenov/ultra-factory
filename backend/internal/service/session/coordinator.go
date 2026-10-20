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

// remoteLinkLister reads the remote worker links that count toward capacity.
type remoteLinkLister interface {
	ListRemoteWorkerLinks(ctx context.Context) ([]domain.RemoteWorkerLink, error)
}

// capacityCounts is a project's active census: sessions until terminated,
// remote links while they exist.
type capacityCounts struct {
	coordinators int
	workers      int
	byOwner      map[domain.SessionID]int
}

func (s *Service) countCapacity(ctx context.Context, project domain.ProjectID) (capacityCounts, error) {
	counts := capacityCounts{byOwner: map[domain.SessionID]int{}}
	recs, err := s.store.ListSessions(ctx, project)
	if err != nil {
		return counts, fmt.Errorf("count capacity for %s: %w", project, err)
	}
	for _, rec := range recs {
		if rec.IsTerminated || rec.IsTaskPreparation {
			continue
		}
		switch rec.Kind {
		case domain.KindCoordinator:
			counts.coordinators++
		case domain.KindWorker:
			counts.workers++
			if rec.OwnerSessionID != nil {
				counts.byOwner[*rec.OwnerSessionID]++
			}
		}
	}
	lister, ok := s.store.(remoteLinkLister)
	if !ok {
		return counts, nil
	}
	links, err := lister.ListRemoteWorkerLinks(ctx)
	if err != nil {
		return counts, fmt.Errorf("count remote links for %s: %w", project, err)
	}
	for _, link := range links {
		if link.ProjectID != project {
			continue
		}
		counts.workers++
		if link.OwnerSessionID != nil {
			counts.byOwner[*link.OwnerSessionID]++
		}
	}
	return counts, nil
}

// checkOwnerCapacity rejects a worker that would be the coordinator's fourth.
func (c capacityCounts) checkOwnerCapacity(owner *domain.SessionID) error {
	if owner != nil && c.byOwner[*owner] >= coordownership.MaxWorkersPerCoordinator {
		return coordownership.CoordinatorWorkerLimitError(*owner, c.byOwner[*owner])
	}
	return nil
}

// checkNewWorker applies the owner cap and project total to one more worker.
// The total binds only once a coordinator is active.
func (c capacityCounts) checkNewWorker(project domain.ProjectID, owner *domain.SessionID) error {
	if c.coordinators == 0 {
		return nil
	}
	if err := c.checkOwnerCapacity(owner); err != nil {
		return err
	}
	if c.workers >= coordownership.MaxProjectWorkers {
		return coordownership.ProjectWorkerLimitError(project, c.workers)
	}
	return nil
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
	if counts.coordinators == 0 && owner == nil {
		return mu.RUnlock, nil
	}
	mu.RUnlock()
	mu.Lock()
	counts, err = s.countCapacity(ctx, project)
	if err == nil {
		err = counts.checkNewWorker(project, owner)
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
	if counts.coordinators >= coordownership.MaxCoordinators {
		return domain.Session{}, 0, 0, coordownership.CoordinatorLimitError(cfg.ProjectID, counts.coordinators)
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
		if err == nil && counts.coordinators >= coordownership.MaxCoordinators {
			err = coordownership.CoordinatorLimitError(rec.ProjectID, counts.coordinators)
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
