package ownership

import (
	"context"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// WorkerReferenceStore reads local workers and remote worker links.
type WorkerReferenceStore interface {
	ListSessions(context.Context, domain.ProjectID) ([]domain.SessionRecord, error)
	ListRemoteWorkerLinks(context.Context) ([]domain.RemoteWorkerLink, error)
}

// WorkerReference is a local session id or a host-qualified remote id.
type WorkerReference struct {
	Host      string
	SessionID domain.SessionID
	Invalid   bool
}

// WorkerTarget is one matching local session or remote worker link.
type WorkerTarget struct {
	Local  *domain.SessionRecord
	Remote *domain.RemoteWorkerLink
}

// ParseWorkerReference accepts a session id or host:id. Host may be a saved
// host id or label; session ids themselves do not contain colons.
func ParseWorkerReference(raw string) WorkerReference {
	raw = strings.TrimSpace(raw)
	if host, id, found := strings.Cut(raw, ":"); found {
		host, id = strings.TrimSpace(host), strings.TrimSpace(id)
		return WorkerReference{Host: host, SessionID: domain.SessionID(id), Invalid: host == "" || id == "" || strings.Contains(id, ":")}
	}
	return WorkerReference{SessionID: domain.SessionID(raw)}
}

// FindWorkerTargets resolves a worker reference without making an ownership
// decision. Callers must check the returned target's current owner.
func FindWorkerTargets(
	ctx context.Context,
	store WorkerReferenceStore,
	project domain.ProjectID,
	raw string,
) ([]WorkerTarget, error) {
	ref := ParseWorkerReference(raw)
	if ref.Invalid || ref.SessionID == "" {
		return nil, nil
	}
	targets, err := ListWorkerTargets(ctx, store, project)
	if err != nil {
		return nil, err
	}
	var matches []WorkerTarget
	for _, target := range targets {
		if target.Local != nil {
			if ref.Host == "" && target.Local.ID == ref.SessionID {
				matches = append(matches, target)
			}
			continue
		}
		link := target.Remote
		if link.RemoteSessionID == ref.SessionID && (ref.Host == "" || link.HostID == ref.Host || link.HostLabel == ref.Host) {
			matches = append(matches, target)
		}
	}
	return matches, nil
}

// ListWorkerTargets returns the project's local sessions and linked workers.
func ListWorkerTargets(ctx context.Context, store WorkerReferenceStore, project domain.ProjectID) ([]WorkerTarget, error) {
	if store == nil {
		return nil, nil
	}
	var targets []WorkerTarget
	sessions, err := store.ListSessions(ctx, project)
	if err != nil {
		return nil, err
	}
	for i := range sessions {
		if sessions[i].Kind == domain.KindWorker {
			targets = append(targets, WorkerTarget{Local: &sessions[i]})
		}
	}
	links, err := store.ListRemoteWorkerLinks(ctx)
	if err != nil {
		return nil, err
	}
	for i := range links {
		link := links[i]
		if link.ProjectID != project {
			continue
		}
		linkCopy := link
		targets = append(targets, WorkerTarget{Remote: &linkCopy})
	}
	return targets, nil
}
