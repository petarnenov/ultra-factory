package remoteworker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	coordownership "github.com/aoagents/agent-orchestrator/backend/internal/service/ownership"
)

// RemoveLink drops a linked remote worker by hand; only the live root may.
// The removal is logged and kept so discovery skips the worker until a sync.
func (s *Service) RemoveLink(
	ctx context.Context,
	hostID string,
	remoteSessionID domain.SessionID,
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
	projectLock := s.capacity.Project(link.ProjectID)
	projectLock.Lock()
	defer projectLock.Unlock()
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
	if err := s.store.RemoveRemoteWorkerLink(ctx, hostID, remoteSessionID, s.now()); err != nil {
		return domain.RemoteWorkerLink{}, err
	}
	slog.Info("remote worker link removed by operator",
		"host", hostID, "remoteSession", remoteSessionID, "project", link.ProjectID,
		"requestedBy", requestedBy)
	return link, nil
}

// ReleaseKilled drops the link of a worker this daemon just killed, with no
// root check and no removal memory: discovery skips a terminated worker.
func (s *Service) ReleaseKilled(ctx context.Context, hostID string, remoteSessionID domain.SessionID) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("remoteworker: store is required")
	}
	link, ok, err := s.store.GetRemoteWorkerLink(ctx, hostID, remoteSessionID)
	if err != nil || !ok {
		return err
	}
	projectLock := s.capacity.Project(link.ProjectID)
	projectLock.Lock()
	defer projectLock.Unlock()
	if err := s.store.DeleteRemoteWorkerLink(ctx, hostID, remoteSessionID); err != nil {
		return err
	}
	slog.Info("remote worker link released after kill",
		"host", hostID, "remoteSession", remoteSessionID, "project", link.ProjectID)
	return nil
}
