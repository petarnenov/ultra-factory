package remoteworker

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	coordownership "github.com/aoagents/agent-orchestrator/backend/internal/service/ownership"
)

// AssignOwner sets or clears one linked remote worker's owner; nil is root.
// This is the service boundary CO-2.1.2 exposes; no route is added here yet.
func (s *Service) AssignOwner(
	ctx context.Context,
	hostID string,
	remoteSessionID domain.SessionID,
	ownerID *domain.SessionID,
	requestedBy domain.SessionID,
) (domain.RemoteWorkerLink, error) {
	if s == nil || s.store == nil {
		return domain.RemoteWorkerLink{}, fmt.Errorf("remoteworker: store is required")
	}
	link, ok, err := s.store.GetRemoteWorkerLink(ctx, hostID, remoteSessionID)
	if err != nil {
		return domain.RemoteWorkerLink{}, err
	}
	if !ok {
		return domain.RemoteWorkerLink{}, apierr.NotFound("REMOTE_WORKER_LINK_NOT_FOUND", "Unknown remote worker link")
	}
	caller, ok, err := s.store.GetSession(ctx, requestedBy)
	if err != nil {
		return domain.RemoteWorkerLink{}, err
	}
	if !ok {
		return domain.RemoteWorkerLink{}, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
	}
	if err := coordownership.RequireLiveRoot(caller, link.ProjectID); err != nil {
		return domain.RemoteWorkerLink{}, err
	}
	if ownerID != nil {
		owner, ok, err := s.store.GetSession(ctx, *ownerID)
		if err != nil {
			return domain.RemoteWorkerLink{}, err
		}
		if !ok {
			return domain.RemoteWorkerLink{}, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
		}
		if err := coordownership.RequireCoordinator(owner, link.ProjectID); err != nil {
			return domain.RemoteWorkerLink{}, err
		}
	}
	reason := domain.OwnershipExplicitReassignment
	if link.OwnerSessionID == nil && ownerID != nil {
		reason = domain.OwnershipExplicitAssignment
	}
	updated, _, err := s.store.SetRemoteWorkerOwner(ctx, hostID, remoteSessionID, ownerID, reason, s.now())
	if err != nil {
		return domain.RemoteWorkerLink{}, err
	}
	return updated, nil
}
