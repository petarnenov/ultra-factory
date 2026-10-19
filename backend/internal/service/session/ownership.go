package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
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
	record, _, err := s.store.SetSessionOwner(ctx, worker.ID, ownerID, domain.OwnershipExplicitAssignment, s.now(), false)
	if errors.Is(err, ports.ErrOwnerReassignmentInFlight) {
		return domain.SessionRecord{}, apierr.Conflict("OWNER_REASSIGNMENT_IN_FLIGHT", "A report of this worker is claimed for delivery", map[string]any{"sessionId": string(worker.ID)})
	}
	if err != nil {
		return domain.SessionRecord{}, err
	}
	return record, nil
}
