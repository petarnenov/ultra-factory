package remoteworker

import (
	"context"
	"fmt"
	"sort"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	coordownership "github.com/aoagents/agent-orchestrator/backend/internal/service/ownership"
)

// AssignOwner sets or clears one linked remote worker's owner; nil is root.
// This is the service boundary the remote ownership write route calls.
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
	projectLock := s.capacity.Project(link.ProjectID)
	projectLock.Lock()
	defer projectLock.Unlock()
	if link, ok, err = s.store.GetRemoteWorkerLink(ctx, hostID, remoteSessionID); err != nil {
		return domain.RemoteWorkerLink{}, err
	} else if !ok {
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
		if link.OwnerSessionID == nil || *link.OwnerSessionID != *ownerID {
			counts, err := coordownership.Count(ctx, s.store, s.store, link.ProjectID)
			if err != nil {
				return domain.RemoteWorkerLink{}, err
			}
			gate, err := coordownership.LoadConfig(ctx, s.store, link.ProjectID)
			if err != nil {
				return domain.RemoteWorkerLink{}, err
			}
			if err := counts.CheckOwner(ownerID, gate.Limits()); err != nil {
				return domain.RemoteWorkerLink{}, err
			}
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

// List returns the links this daemon polls, optionally scoped to a project.
func (s *Service) List(ctx context.Context, project domain.ProjectID) ([]domain.RemoteWorkerLink, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("remoteworker: store is required")
	}
	links, err := s.store.ListRemoteWorkerLinks(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.RemoteWorkerLink, 0, len(links))
	for _, link := range links {
		if project == "" || link.ProjectID == project {
			out = append(out, link)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].HostID != out[j].HostID {
			return out[i].HostID < out[j].HostID
		}
		return out[i].RemoteSessionID < out[j].RemoteSessionID
	})
	return out, nil
}
