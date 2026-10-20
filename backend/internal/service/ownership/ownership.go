// Package ownership holds ADR 0010's shared ownership validation rules.
// SQLite triggers in migration 0212 are the durable backstop for other writers.
package ownership

import (
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

// IsLiveRoot reports whether rec is the project's live root orchestrator.
func IsLiveRoot(rec domain.SessionRecord, project domain.ProjectID) bool {
	return rec.Kind == domain.KindOrchestrator && !rec.IsTerminated && rec.ProjectID == project
}

// IsLiveCoordinator reports whether rec is a live coordinator in project.
func IsLiveCoordinator(rec domain.SessionRecord, project domain.ProjectID) bool {
	return rec.Kind == domain.KindCoordinator && !rec.IsTerminated && rec.ProjectID == project
}

// RequireLiveRoot accepts only the project's live root as an ownership writer.
func RequireLiveRoot(rec domain.SessionRecord, project domain.ProjectID) error {
	if IsLiveRoot(rec, project) {
		return nil
	}
	return apierr.New(apierr.KindForbidden, "OWNERSHIP_ROOT_ONLY",
		"Only the project root may change worker ownership", map[string]any{"sessionId": string(rec.ID)})
}

// RequireMergeAuthority refuses a merge requested by a live coordinator or
// worker. Any other requester, including an unknown or terminated session,
// is not refused; the service skips the check when no session is named.
func RequireMergeAuthority(rec domain.SessionRecord) error {
	if rec.IsTerminated || (rec.Kind != domain.KindCoordinator && rec.Kind != domain.KindWorker) {
		return nil
	}
	return apierr.New(apierr.KindForbidden, "MERGE_ROOT_ONLY",
		"Only the project root may merge a pull request",
		map[string]any{"sessionId": string(rec.ID), "role": string(rec.Kind)})
}

// SpawnOwner derives a worker's owner from its spawn parent: only a live
// same-project coordinator owns it, everything else leaves it root-owned.
func SpawnOwner(parent domain.SessionRecord, project domain.ProjectID) *domain.SessionID {
	if !IsLiveCoordinator(parent, project) {
		return nil
	}
	id := parent.ID
	return &id
}

// RequireWorkerTarget rejects an ownership write on a non-worker session.
func RequireWorkerTarget(target domain.SessionRecord) error {
	if target.Kind != domain.KindWorker {
		return apierr.Invalid("OWNER_TARGET_NOT_WORKER", "Ownership belongs only to worker sessions", map[string]any{"sessionId": string(target.ID)})
	}
	return nil
}

// RequireCoordinator validates a non-NULL owner against invariant I4: the
// owner is a live coordinator in the worker's project.
func RequireCoordinator(owner domain.SessionRecord, project domain.ProjectID) error {
	if owner.ProjectID != project {
		return apierr.Conflict("OWNER_CROSS_PROJECT", "The owner belongs to another project", map[string]any{"ownerSessionId": string(owner.ID)})
	}
	if owner.Kind != domain.KindCoordinator {
		return apierr.Conflict("OWNER_NOT_COORDINATOR", "The owner is not a coordinator", map[string]any{"ownerSessionId": string(owner.ID)})
	}
	if owner.IsTerminated {
		return apierr.Conflict("OWNER_TERMINATED", "The owner is terminated", map[string]any{"ownerSessionId": string(owner.ID)})
	}
	return nil
}
