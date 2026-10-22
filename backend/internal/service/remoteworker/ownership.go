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
// Owners are notified after the write, outside the project lock.
func (s *Service) AssignOwner(
	ctx context.Context,
	hostID string,
	remoteSessionID domain.SessionID,
	ownerID *domain.SessionID,
	requestedBy domain.SessionID,
) (domain.RemoteWorkerLink, error) {
	updated, change, err := s.assignOwner(ctx, hostID, remoteSessionID, ownerID, requestedBy)
	if err != nil {
		return domain.RemoteWorkerLink{}, err
	}
	if s.notifier != nil {
		coordownership.Notify(ctx, s.notifier, s.logger, change)
	}
	return updated, nil
}

// assignOwner performs the locked, validated owner write and describes it.
func (s *Service) assignOwner(
	ctx context.Context,
	hostID string,
	remoteSessionID domain.SessionID,
	ownerID *domain.SessionID,
	requestedBy domain.SessionID,
) (domain.RemoteWorkerLink, coordownership.Change, error) {
	if s == nil || s.store == nil {
		return domain.RemoteWorkerLink{}, coordownership.Change{}, fmt.Errorf("remoteworker: store is required")
	}
	link, ok, err := s.store.GetRemoteWorkerLink(ctx, hostID, remoteSessionID)
	if err != nil {
		return domain.RemoteWorkerLink{}, coordownership.Change{}, err
	}
	if !ok {
		return domain.RemoteWorkerLink{}, coordownership.Change{}, apierr.NotFound("REMOTE_WORKER_LINK_NOT_FOUND", "Unknown remote worker link")
	}
	projectLock := s.capacity.Project(link.ProjectID)
	projectLock.Lock()
	defer projectLock.Unlock()
	if link, ok, err = s.store.GetRemoteWorkerLink(ctx, hostID, remoteSessionID); err != nil {
		return domain.RemoteWorkerLink{}, coordownership.Change{}, err
	} else if !ok {
		return domain.RemoteWorkerLink{}, coordownership.Change{}, apierr.NotFound("REMOTE_WORKER_LINK_NOT_FOUND", "Unknown remote worker link")
	}
	caller, ok, err := s.store.GetSession(ctx, requestedBy)
	if err != nil {
		return domain.RemoteWorkerLink{}, coordownership.Change{}, err
	}
	if !ok {
		return domain.RemoteWorkerLink{}, coordownership.Change{}, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
	}
	if err := coordownership.RequireLiveRoot(caller, link.ProjectID); err != nil {
		return domain.RemoteWorkerLink{}, coordownership.Change{}, err
	}
	if ownerID != nil {
		owner, ok, err := s.store.GetSession(ctx, *ownerID)
		if err != nil {
			return domain.RemoteWorkerLink{}, coordownership.Change{}, err
		}
		if !ok {
			return domain.RemoteWorkerLink{}, coordownership.Change{}, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
		}
		if err := coordownership.RequireCoordinator(owner, link.ProjectID); err != nil {
			return domain.RemoteWorkerLink{}, coordownership.Change{}, err
		}
		if link.OwnerSessionID == nil || *link.OwnerSessionID != *ownerID {
			counts, err := coordownership.Count(ctx, s.store, s.store, link.ProjectID)
			if err != nil {
				return domain.RemoteWorkerLink{}, coordownership.Change{}, err
			}
			gate, err := coordownership.LoadConfig(ctx, s.store, link.ProjectID)
			if err != nil {
				return domain.RemoteWorkerLink{}, coordownership.Change{}, err
			}
			if err := counts.CheckOwner(ownerID, gate.Limits()); err != nil {
				return domain.RemoteWorkerLink{}, coordownership.Change{}, err
			}
			if err := counts.CheckHost(ownerID, link.HostID, gate); err != nil {
				return domain.RemoteWorkerLink{}, coordownership.Change{}, err
			}
		}
	}
	reason := domain.OwnershipExplicitReassignment
	if link.OwnerSessionID == nil && ownerID != nil {
		reason = domain.OwnershipExplicitAssignment
	}
	change := coordownership.Change{Worker: remoteSessionID, Previous: link.OwnerSessionID, Next: ownerID}
	if change.Previous != nil {
		prev, found, err := s.store.GetSession(ctx, *change.Previous)
		if err != nil {
			return domain.RemoteWorkerLink{}, coordownership.Change{}, err
		}
		change.PreviousLive = found && coordownership.IsLiveCoordinator(prev, link.ProjectID)
	}
	updated, _, err := s.store.SetRemoteWorkerOwner(ctx, hostID, remoteSessionID, ownerID, reason, s.now())
	if err != nil {
		return domain.RemoteWorkerLink{}, coordownership.Change{}, err
	}
	return updated, change, nil
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
