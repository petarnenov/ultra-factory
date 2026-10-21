// Package remoteworker records which workers on saved remote AO hosts report
// to a project on this daemon. The remotereports observer polls those links.
package remoteworker

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
	coordownership "github.com/aoagents/agent-orchestrator/backend/internal/service/ownership"
)

// Store is the link persistence and parent-session boundary.
type Store interface {
	GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
	GetProject(context.Context, string) (domain.ProjectRecord, bool, error)
	ListSessions(context.Context, domain.ProjectID) ([]domain.SessionRecord, error)
	ListRemoteWorkerLinks(context.Context) ([]domain.RemoteWorkerLink, error)
	GetRemoteWorkerLink(context.Context, string, domain.SessionID) (domain.RemoteWorkerLink, bool, error)
	UpsertRemoteWorkerLink(context.Context, domain.RemoteWorkerLink) error
	SetRemoteWorkerOwner(context.Context, string, domain.SessionID, *domain.SessionID, domain.OwnershipEventReason, time.Time) (domain.RemoteWorkerLink, bool, error)
}

// MaxLinks bounds how many remote workers this daemon polls; every link costs
// authenticated requests to its host on each pass.
const MaxLinks = 200

// remoteIDPattern is the shape of AO session and project IDs. Remote IDs are
// rendered into the orchestrator's prompt, so nothing else is accepted.
var remoteIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// Registration is a stored link and, when this daemon will not poll its host
// unattended, why.
type Registration struct {
	Link        domain.RemoteWorkerLink
	PullBlocked string
}

// RegisterInput is what `ao spawn --host` reports after a remote spawn.
type RegisterInput struct {
	// ParentSessionID is the local session that ran the spawn; its project
	// receives the remote worker's reports.
	ParentSessionID domain.SessionID
	HostID          string
	RemoteSessionID domain.SessionID
	RemoteProjectID domain.ProjectID
	DisplayName     string
}

// Deps configures a Service. Lookup defaults to the saved-host file; OnLinked
// is optional and runs after a link is stored.
type Deps struct {
	Store      Store
	SelfHostID string
	Lookup     func(hostID string) (remotehost.Entry, bool, error)
	Now        func() time.Time
	OnLinked   func()
	// Capacity is shared with the session service so link writes and local
	// spawns serialise per project. Nil gives this service its own guard.
	Capacity *coordownership.Guard
}

// Service validates and stores remote worker links.
type Service struct {
	store      Store
	selfHostID string
	lookup     func(string) (remotehost.Entry, bool, error)
	now        func() time.Time
	onLinked   func()
	capacity   *coordownership.Guard
	// mu makes the link-limit check and the insert one step.
	mu sync.Mutex
}

// New constructs a Service.
func New(d Deps) *Service {
	if d.Lookup == nil {
		d.Lookup = remotehost.Find
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Capacity == nil {
		d.Capacity = &coordownership.Guard{}
	}
	return &Service{store: d.Store, selfHostID: d.SelfHostID, lookup: d.Lookup, now: d.Now, onLinked: d.OnLinked, capacity: d.Capacity}
}

// MaxDisplayNameLength matches the spawn display-name limit.
const MaxDisplayNameLength = 100

// Register links a remote worker to the parent session's project. The host
// must be a saved, paired host other than this machine; its label comes from
// the saved entry, never from the caller.
func (s *Service) Register(ctx context.Context, in RegisterInput) (Registration, error) {
	if s == nil || s.store == nil {
		return Registration{}, errors.New("remoteworker: store is required")
	}
	in.HostID = strings.TrimSpace(in.HostID)
	in.DisplayName = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, in.DisplayName))
	if in.ParentSessionID == "" || in.HostID == "" || !remoteIDPattern.MatchString(string(in.RemoteSessionID)) ||
		!remoteIDPattern.MatchString(string(in.RemoteProjectID)) || len([]rune(in.DisplayName)) > MaxDisplayNameLength {
		return Registration{}, apierr.Invalid("INVALID_REMOTE_WORKER_LINK",
			"parentSessionId and hostId are required; remoteSessionId and remoteProjectId must be AO ids; displayName is at most 100 characters", nil)
	}
	if s.selfHostID != "" && in.HostID == s.selfHostID {
		return Registration{}, apierr.Invalid("REMOTE_HOST_IS_LOCAL", "The host is this machine; local workers need no link", nil)
	}
	parent, err := s.linkParent(ctx, in.ParentSessionID)
	if err != nil {
		return Registration{}, err
	}
	entry, found, err := s.lookup(in.HostID)
	if err != nil {
		return Registration{}, err
	}
	if !found {
		return Registration{}, apierr.Invalid("REMOTE_HOST_UNKNOWN", "No saved remote host has this host id", nil)
	}
	link := domain.RemoteWorkerLink{
		HostID: in.HostID, HostLabel: strings.TrimSpace(entry.Label),
		RemoteSessionID: in.RemoteSessionID, RemoteProjectID: in.RemoteProjectID,
		ProjectID: parent.ProjectID, DisplayName: in.DisplayName,
		OwnerSessionID: coordownership.SpawnOwner(parent, parent.ProjectID),
		CreatedAt:      s.now().UTC(),
	}
	projectLock := s.capacity.Project(link.ProjectID)
	projectLock.Lock()
	defer projectLock.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, err := s.store.ListRemoteWorkerLinks(ctx)
	if err != nil {
		return Registration{}, err
	}
	if current, found := findLink(existing, link); found {
		// A link's local project and owner are fixed at creation; moving the
		// project would strand a non-NULL owner outside it and violate I4.
		if current.ProjectID != link.ProjectID {
			return Registration{}, apierr.Conflict("REMOTE_WORKER_LINK_PROJECT_CONFLICT",
				"The remote worker link already belongs to a different project",
				map[string]any{"hostId": in.HostID, "remoteSessionId": string(in.RemoteSessionID), "projectId": string(link.ProjectID)})
		}
	}
	if len(existing) >= MaxLinks && !hasLink(existing, link) {
		return Registration{}, apierr.Conflict("REMOTE_WORKER_LINK_LIMIT", "Too many remote workers are linked; finished workers are unlinked automatically", nil)
	}
	if !hasLink(existing, link) {
		if err := s.checkCapacity(ctx, link.ProjectID, link.OwnerSessionID, link.HostID); err != nil {
			return Registration{}, err
		}
	}
	if err := s.store.UpsertRemoteWorkerLink(ctx, link); err != nil {
		return Registration{}, err
	}
	stored := link
	if current, found, err := s.store.GetRemoteWorkerLink(ctx, link.HostID, link.RemoteSessionID); err != nil {
		return Registration{}, err
	} else if found {
		stored = current
	}
	if s.onLinked != nil {
		s.onLinked()
	}
	registration := Registration{Link: stored}
	base, err := remotehost.BaseURL(entry.URL)
	if err == nil {
		err = remotehost.UnattendedTransportAllowed(base, entry.AllowInsecureHTTP)
	}
	if err != nil {
		registration.PullBlocked = err.Error()
	}
	return registration, nil
}

// linkParent loads the live, project-bound session that spawns a remote worker.
func (s *Service) linkParent(ctx context.Context, id domain.SessionID) (domain.SessionRecord, error) {
	parent, ok, err := s.store.GetSession(ctx, id)
	if err != nil {
		return domain.SessionRecord{}, err
	}
	if !ok {
		return domain.SessionRecord{}, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
	}
	if parent.IsTerminated {
		return domain.SessionRecord{}, apierr.Conflict("PARENT_SESSION_TERMINATED", "The parent session is terminated", nil)
	}
	if parent.ProjectID == "" {
		return domain.SessionRecord{}, apierr.Invalid("PARENT_PROJECT_REQUIRED", "The parent session has no project", nil)
	}
	return parent, nil
}

// CheckCapacity reports whether a worker of parentID on hostID could be
// linked now; an empty hostID skips host rules. Register re-validates.
func (s *Service) CheckCapacity(ctx context.Context, parentID domain.SessionID, hostID string) error {
	if s == nil || s.store == nil {
		return errors.New("remoteworker: store is required")
	}
	parent, err := s.linkParent(ctx, parentID)
	if err != nil {
		return err
	}
	projectLock := s.capacity.Project(parent.ProjectID)
	projectLock.Lock()
	defer projectLock.Unlock()
	return s.checkCapacity(ctx, parent.ProjectID, coordownership.SpawnOwner(parent, parent.ProjectID), strings.TrimSpace(hostID))
}

func (s *Service) checkCapacity(ctx context.Context, projectID domain.ProjectID, owner *domain.SessionID, hostID string) error {
	counts, err := coordownership.Count(ctx, s.store, s.store, projectID)
	if err != nil {
		return err
	}
	gate, err := coordownership.LoadConfig(ctx, s.store, projectID)
	if err != nil {
		return err
	}
	if hostID == "" {
		return counts.CheckNewWorker(projectID, owner, gate.Limits())
	}
	return counts.CheckNewRemoteWorker(projectID, owner, hostID, gate)
}

func hasLink(links []domain.RemoteWorkerLink, link domain.RemoteWorkerLink) bool {
	_, found := findLink(links, link)
	return found
}

func findLink(links []domain.RemoteWorkerLink, link domain.RemoteWorkerLink) (domain.RemoteWorkerLink, bool) {
	for _, existing := range links {
		if existing.HostID == link.HostID && existing.RemoteSessionID == link.RemoteSessionID {
			return existing, true
		}
	}
	return domain.RemoteWorkerLink{}, false
}
