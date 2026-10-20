package ownership

import (
	"context"
	"fmt"
	"sync"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

// Initial rollout limits from ADR 0010 section 8.
const (
	MaxCoordinators          = 2
	MaxWorkersPerCoordinator = 3
	MaxProjectWorkers        = 6
	MaxScopeLength           = 200
)

// RequireRootCaller accepts only the project's live root as the spawn caller
// of a coordinator. An absent or unknown caller is refused too.
func RequireRootCaller(caller domain.SessionRecord, found bool, id domain.SessionID, project domain.ProjectID) error {
	if found && IsLiveRoot(caller, project) {
		return nil
	}
	return apierr.New(apierr.KindForbidden, "COORDINATOR_ROOT_REQUIRED",
		"Only the project root may spawn a coordinator", map[string]any{"sessionId": string(id)})
}

// NormalizeScope validates a coordinator scope and returns its stored form.
func NormalizeScope(scope string) (string, error) {
	scope = domain.NormalizeCoordinatorScope(scope)
	if scope == "" {
		return "", apierr.Invalid("COORDINATOR_SCOPE_REQUIRED", "A coordinator requires a non-empty scope", nil)
	}
	if utf8.RuneCountInString(scope) > MaxScopeLength {
		return "", apierr.Invalid("COORDINATOR_SCOPE_TOO_LONG", "A coordinator scope must be 200 characters or fewer",
			map[string]any{"maxLength": MaxScopeLength})
	}
	return scope, nil
}

// CoordinatorLimitError reports the project's coordinator cap.
func CoordinatorLimitError(project domain.ProjectID, current int) error {
	return apierr.Conflict("COORDINATOR_LIMIT_REACHED", "The project already has the maximum number of active coordinators",
		map[string]any{"projectId": string(project), "current": current, "allowed": MaxCoordinators})
}

// CoordinatorWorkerLimitError reports one coordinator's worker cap.
func CoordinatorWorkerLimitError(coordinator domain.SessionID, current int) error {
	return apierr.Conflict("COORDINATOR_WORKER_LIMIT_REACHED", "The coordinator already has the maximum number of active workers",
		map[string]any{"coordinatorId": string(coordinator), "current": current, "allowed": MaxWorkersPerCoordinator})
}

// ProjectWorkerLimitError reports the project-wide worker cap.
func ProjectWorkerLimitError(project domain.ProjectID, current int) error {
	return apierr.Conflict("PROJECT_WORKER_LIMIT_REACHED", "The project already has the maximum number of active workers",
		map[string]any{"projectId": string(project), "current": current, "allowed": MaxProjectWorkers})
}

// Guard hands out one lock per project for every write that can add an active
// worker or coordinator. Share one Guard between services.
type Guard struct {
	mu    sync.Mutex
	locks map[domain.ProjectID]*sync.RWMutex
}

// Project returns the project's lock.
func (g *Guard) Project(project domain.ProjectID) *sync.RWMutex {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.locks == nil {
		g.locks = make(map[domain.ProjectID]*sync.RWMutex)
	}
	lock := g.locks[project]
	if lock == nil {
		lock = &sync.RWMutex{}
		g.locks[project] = lock
	}
	return lock
}

// SessionLister reads a project's sessions.
type SessionLister interface {
	ListSessions(ctx context.Context, project domain.ProjectID) ([]domain.SessionRecord, error)
}

// LinkLister reads the remote worker links.
type LinkLister interface {
	ListRemoteWorkerLinks(ctx context.Context) ([]domain.RemoteWorkerLink, error)
}

// Counts is a project's active census: sessions until terminated, remote
// links while they exist.
type Counts struct {
	Coordinators int
	Workers      int
	ByOwner      map[domain.SessionID]int
}

// Count reads the census. links may be nil when the store has no links.
func Count(ctx context.Context, sessions SessionLister, links LinkLister, project domain.ProjectID) (Counts, error) {
	counts := Counts{ByOwner: map[domain.SessionID]int{}}
	recs, err := sessions.ListSessions(ctx, project)
	if err != nil {
		return counts, fmt.Errorf("count capacity for %s: %w", project, err)
	}
	for _, rec := range recs {
		if rec.IsTerminated || rec.IsTaskPreparation {
			continue
		}
		switch rec.Kind {
		case domain.KindCoordinator:
			counts.Coordinators++
		case domain.KindWorker:
			counts.Workers++
			if rec.OwnerSessionID != nil {
				counts.ByOwner[*rec.OwnerSessionID]++
			}
		}
	}
	if links == nil {
		return counts, nil
	}
	all, err := links.ListRemoteWorkerLinks(ctx)
	if err != nil {
		return counts, fmt.Errorf("count remote links for %s: %w", project, err)
	}
	for _, link := range all {
		if link.ProjectID != project {
			continue
		}
		counts.Workers++
		if link.OwnerSessionID != nil {
			counts.ByOwner[*link.OwnerSessionID]++
		}
	}
	return counts, nil
}

// CheckOwner rejects a worker that would be the coordinator's fourth.
func (c Counts) CheckOwner(owner *domain.SessionID) error {
	if owner != nil && c.ByOwner[*owner] >= MaxWorkersPerCoordinator {
		return CoordinatorWorkerLimitError(*owner, c.ByOwner[*owner])
	}
	return nil
}

// CheckNewWorker applies the owner cap and project total to one more worker.
// The total binds only once a coordinator is active.
func (c Counts) CheckNewWorker(project domain.ProjectID, owner *domain.SessionID) error {
	if c.Coordinators == 0 {
		return nil
	}
	if err := c.CheckOwner(owner); err != nil {
		return err
	}
	if c.Workers >= MaxProjectWorkers {
		return ProjectWorkerLimitError(project, c.Workers)
	}
	return nil
}
