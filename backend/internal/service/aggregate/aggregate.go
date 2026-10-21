// Package aggregate resolves which remote projects appear on a project's
// aggregate board. Discovery probes the paired hosts in the remote host
// registry and keeps the projects that are the same project as a local one;
// explicit config adds rows and excludes hide them.
package aggregate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
)

// Source values for an effective row.
const (
	SourceExplicit   = "explicit"
	SourceDiscovered = "discovered"
)

// MatchedBy values for a discovered row.
const (
	MatchedByRepo      = "repo"
	MatchedByProjectID = "projectId"
)

// Defaults for the discovery budget and cadence.
const (
	DefaultHostBudget   = 45 * time.Second
	DefaultInterval     = 5 * time.Minute
	DefaultPollInterval = 30 * time.Second
	// MaxProjectsPerHost caps how many remote projects one host may list.
	MaxProjectsPerHost = 200
)

// Entry is one row of a project's effective aggregate set.
type Entry struct {
	HostID    string `json:"hostId"`
	HostLabel string `json:"hostLabel"`
	ProjectID string `json:"projectId"`
	Source    string `json:"source" enum:"explicit,discovered"`
	MatchedBy string `json:"matchedBy,omitempty" enum:"repo,projectId"`
	Stale     bool   `json:"stale"`
}

// RemoteProject is one project a paired host lists, with its repository
// identity when the host exposes one.
type RemoteProject struct {
	ID   string
	Repo string
}

// Remote is an open, identity-checked connection to one paired host.
type Remote interface {
	Projects(ctx context.Context) ([]RemoteProject, error)
	Close()
}

// Deps wires the resolver. Nil fields use the production defaults.
type Deps struct {
	// Hosts lists the paired hosts. Default: the desktop remotes.json.
	Hosts func() ([]remotehost.Entry, error)
	// Open connects to one paired host. Default: remotehost.Open.
	Open func(ctx context.Context, entry remotehost.Entry) (Remote, error)
	// Self is this machine's host ID, which is never discovered.
	Self string
	// HTTPClient is the base client for remote requests.
	HTTPClient *http.Client
	// Budget bounds one host's refresh. Default: 45s.
	Budget time.Duration
	// Interval is the longest time between full passes. Default: 5m.
	Interval time.Duration
	// PollInterval is how often the registry is checked for changes.
	PollInterval time.Duration
	Logger       *slog.Logger
}

// Service caches each paired host's project list and resolves effective sets.
type Service struct {
	deps Deps

	passMu sync.Mutex // one pass at a time

	mu          sync.Mutex // guards the fields below
	hosts       map[string]*hostState
	fingerprint string
	lastPass    time.Time
}

type hostState struct {
	label      string
	projects   []RemoteProject
	stale      bool
	refused    bool
	refusedKey string
}

// New returns a resolver with defaults filled in. It does not start passes.
func New(deps Deps) *Service {
	if deps.Hosts == nil {
		deps.Hosts = readRegistry
	}
	if deps.Open == nil {
		deps.Open = func(ctx context.Context, entry remotehost.Entry) (Remote, error) {
			session, err := remotehost.Open(ctx, deps.HTTPClient, entry, deps.Budget)
			if err != nil {
				return nil, err
			}
			return sessionRemote{session: session}, nil
		}
	}
	if deps.Budget <= 0 {
		deps.Budget = DefaultHostBudget
	}
	if deps.Interval <= 0 {
		deps.Interval = DefaultInterval
	}
	if deps.PollInterval <= 0 {
		deps.PollInterval = DefaultPollInterval
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &Service{deps: deps, hosts: map[string]*hostState{}}
}

func readRegistry() ([]remotehost.Entry, error) {
	path, err := remotehost.FilePath()
	if err != nil {
		return nil, err
	}
	return remotehost.ReadEntries(path)
}

// Start runs a pass now, then passes when the registry changes or Interval
// elapses. It returns a channel closed when ctx ends.
func (s *Service) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Refresh(ctx)
		ticker := time.NewTicker(s.deps.PollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			s.mu.Lock()
			due := time.Since(s.lastPass) >= s.deps.Interval
			s.mu.Unlock()
			if due || s.registryChanged() {
				s.Refresh(ctx)
			}
		}
	}()
	return done
}

func (s *Service) registryChanged() bool {
	entries, err := s.deps.Hosts()
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return registryFingerprint(entries) != s.fingerprint
}

// Refresh runs one discovery pass across every paired host, in parallel. Each
// host is bounded by the budget, so a slow host never delays the others.
func (s *Service) Refresh(ctx context.Context) {
	s.passMu.Lock()
	defer s.passMu.Unlock()
	entries, err := s.deps.Hosts()
	if err != nil {
		s.deps.Logger.Warn("aggregate: read paired hosts", "err", err)
		return
	}
	live := map[string]remotehost.Entry{}
	for _, entry := range entries {
		if entry.HostID == "" || entry.HostID == s.deps.Self {
			continue
		}
		if _, dup := live[entry.HostID]; !dup {
			live[entry.HostID] = entry
		}
	}
	s.mu.Lock()
	for hostID := range s.hosts {
		if _, ok := live[hostID]; !ok {
			delete(s.hosts, hostID)
		}
	}
	s.mu.Unlock()
	var wg sync.WaitGroup
	for _, entry := range live {
		wg.Add(1)
		go func(entry remotehost.Entry) {
			defer wg.Done()
			s.refreshHost(ctx, entry)
		}(entry)
	}
	wg.Wait()
	s.mu.Lock()
	s.fingerprint = registryFingerprint(entries)
	s.lastPass = time.Now()
	s.mu.Unlock()
}

// refreshHost updates one host's snapshot. A host that refused the password
// is skipped until its saved entry changes.
func (s *Service) refreshHost(ctx context.Context, entry remotehost.Entry) {
	key := entryFingerprint(entry)
	s.mu.Lock()
	state := s.ensure(entry)
	if state.refused && state.refusedKey == key {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	hostCtx, cancel := context.WithTimeout(ctx, s.deps.Budget)
	defer cancel()
	projects, err := s.fetch(hostCtx, entry)

	s.mu.Lock()
	defer s.mu.Unlock()
	state = s.ensure(entry)
	switch {
	case err == nil:
		state.projects, state.stale, state.refused, state.refusedKey = projects, false, false, ""
	case errors.Is(err, remotehost.ErrCredentialRefused):
		state.projects, state.stale, state.refused, state.refusedKey = nil, false, true, key
		s.deps.Logger.Warn("aggregate: host refused the saved password", "host", entry.HostID)
	default:
		state.stale = true
		s.deps.Logger.Debug("aggregate: host unreachable, keeping last set", "host", entry.HostID, "err", err)
	}
}

func (s *Service) fetch(ctx context.Context, entry remotehost.Entry) ([]RemoteProject, error) {
	remote, err := s.deps.Open(ctx, entry)
	if err != nil {
		return nil, err
	}
	defer remote.Close()
	return remote.Projects(ctx)
}

// ensure returns the host's state, creating it on first sight. The caller
// must hold s.mu.
func (s *Service) ensure(entry remotehost.Entry) *hostState {
	state := s.hosts[entry.HostID]
	if state == nil {
		state = &hostState{}
		s.hosts[entry.HostID] = state
	}
	state.label = entry.Label
	if state.label == "" {
		state.label = entry.HostID
	}
	return state
}

// Effective returns discovered ∪ explicit − excluded for one local project.
// It reads cached snapshots only and never calls a remote host.
func (s *Service) Effective(projectID domain.ProjectID, repo string, cfg domain.ProjectConfig) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := map[string]*Entry{}
	var order []string
	add := func(entry Entry) {
		key := refKey(entry.HostID, entry.ProjectID)
		if _, ok := rows[key]; !ok {
			order = append(order, key)
		}
		rows[key] = &entry
	}
	if cfg.AggregateDiscoveryEnabled() {
		for _, hostID := range sortedKeys(s.hosts) {
			state := s.hosts[hostID]
			if state.refused {
				continue
			}
			for _, remote := range state.projects {
				matchedBy, ok := matchProject(repo, string(projectID), remote)
				if !ok {
					continue
				}
				add(Entry{HostID: hostID, HostLabel: state.label, ProjectID: remote.ID, Source: SourceDiscovered, MatchedBy: matchedBy, Stale: state.stale})
			}
		}
	}
	for _, ref := range cfg.AggregateProjects {
		key := refKey(ref.HostID, ref.ProjectID)
		if row, ok := rows[key]; ok {
			row.Source = SourceExplicit
			continue
		}
		label, stale := ref.HostID, true
		if state, ok := s.hosts[ref.HostID]; ok {
			label, stale = state.label, state.stale || state.refused
		}
		add(Entry{HostID: ref.HostID, HostLabel: label, ProjectID: ref.ProjectID, Source: SourceExplicit, Stale: stale})
	}
	excluded := map[string]bool{}
	for _, ref := range cfg.AggregateExcludes {
		excluded[refKey(ref.HostID, ref.ProjectID)] = true
	}
	out := make([]Entry, 0, len(order))
	for _, key := range order {
		if excluded[key] {
			continue
		}
		out = append(out, *rows[key])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].HostLabel != out[j].HostLabel {
			return out[i].HostLabel < out[j].HostLabel
		}
		return out[i].ProjectID < out[j].ProjectID
	})
	return out
}

// matchProject applies the two rules in order. Identical normalised repository
// identity wins. When either side lacks one, identical project IDs match.
// Two different repositories never match by ID.
func matchProject(localRepo, localID string, remote RemoteProject) (string, bool) {
	local, other := NormalizeRepo(localRepo), NormalizeRepo(remote.Repo)
	if local != "" && other != "" {
		return MatchedByRepo, local == other
	}
	return MatchedByProjectID, remote.ID == localID
}

// NormalizeRepo reduces a repository URL or scp-style address to host/path so
// https, ssh, and scp forms of one repository compare equal. It lowercases,
// drops a trailing slash and a .git suffix, and drops any user@.
func NormalizeRepo(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return ""
	}
	var host, path string
	if idx := strings.Index(value, "://"); idx >= 0 {
		rest := value[idx+3:]
		if slash := strings.Index(rest, "/"); slash >= 0 {
			host, path = rest[:slash], rest[slash:]
		} else {
			host = rest
		}
		host = host[strings.LastIndex(host, "@")+1:]
	} else if colon := strings.Index(value, ":"); colon > 0 && !strings.Contains(value[:colon], "/") {
		host = value[:colon]
		host = host[strings.LastIndex(host, "@")+1:]
		path = "/" + value[colon+1:]
	} else {
		path = value
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	path = strings.Trim(path, "/")
	if host == "" {
		return "/" + path
	}
	return host + "/" + path
}

// sessionRemote lists a paired host's projects through an open session.
type sessionRemote struct {
	session *remotehost.Session
}

func (r sessionRemote) Close() { r.session.Close() }

func (r sessionRemote) Projects(ctx context.Context) ([]RemoteProject, error) {
	var list struct {
		Projects []struct {
			ID string `json:"id"`
		} `json:"projects"`
	}
	if err := r.session.GetJSON(ctx, "/api/v1/projects", &list); err != nil {
		return nil, err
	}
	if len(list.Projects) > MaxProjectsPerHost {
		list.Projects = list.Projects[:MaxProjectsPerHost]
	}
	out := make([]RemoteProject, 0, len(list.Projects))
	for _, project := range list.Projects {
		var detail struct {
			Project struct {
				Repo string `json:"repo"`
			} `json:"project"`
		}
		if err := r.session.GetJSON(ctx, "/api/v1/projects/"+project.ID, &detail); err != nil {
			return nil, err
		}
		out = append(out, RemoteProject{ID: project.ID, Repo: detail.Project.Repo})
	}
	return out, nil
}

func refKey(hostID, projectID string) string {
	return hostID + "\x00" + projectID
}

func entryFingerprint(entry remotehost.Entry) string {
	sum := sha256.Sum256([]byte(entry.HostID + "\x00" + entry.URL + "\x00" + entry.Password))
	return hex.EncodeToString(sum[:])
}

func registryFingerprint(entries []remotehost.Entry) string {
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts = append(parts, entryFingerprint(entry))
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

func sortedKeys(m map[string]*hostState) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
