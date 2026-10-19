package session

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	coordownership "github.com/aoagents/agent-orchestrator/backend/internal/service/ownership"
)

// AssignOwner sets or clears one worker's owner; nil means root-owned.
// This is the service boundary CO-2.1.2 exposes; no route is added here yet.
func (s *Service) AssignOwner(
	ctx context.Context,
	workerID domain.SessionID,
	ownerID *domain.SessionID,
	requestedBy domain.SessionID,
) (domain.SessionRecord, error) {
	if s.store == nil {
		return domain.SessionRecord{}, fmt.Errorf("assign owner %s: store is required", workerID)
	}
	worker, ok, err := s.store.GetSession(ctx, workerID)
	if err != nil {
		return domain.SessionRecord{}, fmt.Errorf("load worker %s: %w", workerID, err)
	}
	if !ok {
		return domain.SessionRecord{}, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
	}
	if err := coordownership.RequireWorkerTarget(worker); err != nil {
		return domain.SessionRecord{}, err
	}
	if worker.ProjectID == "" {
		return domain.SessionRecord{}, apierr.Invalid("OWNERSHIP_PROJECT_REQUIRED", "Standalone workers have no project ownership", map[string]any{"sessionId": string(worker.ID)})
	}
	caller, ok, err := s.store.GetSession(ctx, requestedBy)
	if err != nil {
		return domain.SessionRecord{}, fmt.Errorf("load ownership caller %s: %w", requestedBy, err)
	}
	if !ok {
		return domain.SessionRecord{}, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
	}
	if err := coordownership.RequireLiveRoot(caller, worker.ProjectID); err != nil {
		return domain.SessionRecord{}, err
	}
	if ownerID != nil {
		owner, ok, err := s.store.GetSession(ctx, *ownerID)
		if err != nil {
			return domain.SessionRecord{}, fmt.Errorf("load owner %s: %w", *ownerID, err)
		}
		if !ok {
			return domain.SessionRecord{}, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
		}
		if err := coordownership.RequireCoordinator(owner, worker.ProjectID); err != nil {
			return domain.SessionRecord{}, err
		}
	}
	if !sameSessionOwner(worker.OwnerSessionID, ownerID) {
		claimed, err := s.store.SessionHasClaimedReport(ctx, worker.ID)
		if err != nil {
			return domain.SessionRecord{}, err
		}
		if claimed {
			return domain.SessionRecord{}, apierr.Conflict("OWNER_REASSIGNMENT_IN_FLIGHT", "A report of this worker is claimed for delivery", map[string]any{"sessionId": string(worker.ID)})
		}
	}
	reason := domain.OwnershipExplicitReassignment
	if worker.OwnerSessionID == nil && ownerID != nil {
		reason = domain.OwnershipExplicitAssignment
	}
	record, _, err := s.store.SetSessionOwner(ctx, worker.ID, ownerID, reason, s.now())
	if err != nil {
		return domain.SessionRecord{}, err
	}
	return record, nil
}

func sameSessionOwner(a, b *domain.SessionID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
