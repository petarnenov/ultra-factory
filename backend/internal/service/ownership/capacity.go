package ownership

import (
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
