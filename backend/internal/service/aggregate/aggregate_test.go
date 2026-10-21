package aggregate

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
)

func TestNormalizeRepo(t *testing.T) {
	cases := map[string]string{
		"":                                         "",
		"https://github.com/Owner/Repo.git":        "github.com/owner/repo",
		"https://github.com/owner/repo":            "github.com/owner/repo",
		"https://github.com/owner/repo/":           "github.com/owner/repo",
		"git@github.com:owner/repo.git":            "github.com/owner/repo",
		"ssh://git@github.com/owner/repo.git":      "github.com/owner/repo",
		"ssh://git@GitHub.com/Owner/Repo.git/":     "github.com/owner/repo",
		"  HTTPS://github.com/owner/repo.GIT  ":    "github.com/owner/repo",
		"https://user:tok@github.com/owner/repo":   "github.com/owner/repo",
		"/srv/git/repo.git":                        "/srv/git/repo",
		"file:///srv/git/repo.git":                 "/srv/git/repo",
		"git@gitlab.example.com:group/sub/app.git": "gitlab.example.com/group/sub/app",
	}
	for raw, want := range cases {
		if got := NormalizeRepo(raw); got != want {
			t.Errorf("NormalizeRepo(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestMatchProjectRules(t *testing.T) {
	cases := []struct {
		name      string
		localRepo string
		localID   string
		remote    RemoteProject
		matched   string
		ok        bool
	}{
		{"same repo different id", "https://github.com/o/r.git", "local", RemoteProject{ID: "other", Repo: "git@github.com:o/r"}, MatchedByRepo, true},
		{"same id different repo", "https://github.com/o/r", "p", RemoteProject{ID: "p", Repo: "https://github.com/x/y"}, MatchedByRepo, false},
		{"remote lacks repo, same id", "https://github.com/o/r", "p", RemoteProject{ID: "p"}, MatchedByProjectID, true},
		{"local lacks repo, same id", "", "p", RemoteProject{ID: "p", Repo: "https://github.com/o/r"}, MatchedByProjectID, true},
		{"local lacks repo, other id", "", "p", RemoteProject{ID: "q", Repo: "https://github.com/o/r"}, MatchedByProjectID, false},
		{"both lack repo, same id", "", "p", RemoteProject{ID: "p"}, MatchedByProjectID, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matched, ok := matchProject(tc.localRepo, tc.localID, tc.remote)
			if matched != tc.matched || ok != tc.ok {
				t.Fatalf("matchProject = (%q, %v), want (%q, %v)", matched, ok, tc.matched, tc.ok)
			}
		})
	}
}

// fakeHosts scripts each paired host's behavior for one pass.
type fakeHosts struct {
	mu       sync.Mutex
	entries  []remotehost.Entry
	behavior map[string]func(ctx context.Context) (Remote, error)
	opens    map[string]int
}

func (f *fakeHosts) list() ([]remotehost.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]remotehost.Entry(nil), f.entries...), nil
}

func (f *fakeHosts) open(ctx context.Context, entry remotehost.Entry) (Remote, error) {
	f.mu.Lock()
	f.opens[entry.HostID]++
	behavior := f.behavior[entry.HostID]
	f.mu.Unlock()
	return behavior(ctx)
}

func reachable(projects ...RemoteProject) func(ctx context.Context) (Remote, error) {
	return func(context.Context) (Remote, error) { return fakeRemote{projects: projects}, nil }
}

func unreachable(context.Context) (Remote, error) { return nil, errors.New("unreachable") }

func refused(context.Context) (Remote, error) { return nil, remotehost.ErrCredentialRefused }

func slow(ctx context.Context) (Remote, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type fakeRemote struct{ projects []RemoteProject }

func (r fakeRemote) Projects(context.Context) ([]RemoteProject, error) { return r.projects, nil }
func (r fakeRemote) Close()                                            {}

func newTestService(hosts *fakeHosts, budget time.Duration) *Service {
	return New(Deps{
		Hosts:  hosts.list,
		Open:   hosts.open,
		Self:   "h_self",
		Budget: budget,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func entry(hostID, label, password string) remotehost.Entry {
	return remotehost.Entry{HostID: hostID, Label: label, URL: "https://" + hostID + ".example", Password: password}
}

func TestEffectiveUnionMinusExcluded(t *testing.T) {
	hosts := &fakeHosts{
		entries: []remotehost.Entry{entry("h_a", "Alpha", "pw"), entry("h_b", "Beta", "pw")},
		behavior: map[string]func(context.Context) (Remote, error){
			"h_a": reachable(RemoteProject{ID: "app-a", Repo: "git@github.com:o/app.git"}, RemoteProject{ID: "unrelated", Repo: "https://github.com/o/other"}),
			"h_b": reachable(RemoteProject{ID: "app", Repo: "https://github.com/O/App"}),
		},
		opens: map[string]int{},
	}
	svc := newTestService(hosts, time.Second)
	svc.Refresh(context.Background())
	cfg := domain.ProjectConfig{
		AggregateProjects: []domain.AggregateProjectRef{{HostID: "h_a", ProjectID: "app-a"}, {HostID: "h_c", ProjectID: "manual"}},
	}
	got := svc.Effective("app", "https://github.com/o/app", cfg)
	want := []Entry{
		{HostID: "h_a", HostLabel: "Alpha", ProjectID: "app-a", Source: SourceExplicit, MatchedBy: MatchedByRepo},
		{HostID: "h_b", HostLabel: "Beta", ProjectID: "app", Source: SourceDiscovered, MatchedBy: MatchedByRepo},
		{HostID: "h_c", HostLabel: "h_c", ProjectID: "manual", Source: SourceExplicit, Stale: true},
	}
	assertEntries(t, got, want)

	cfg.AggregateExcludes = []domain.AggregateProjectRef{{HostID: "h_b", ProjectID: "app"}}
	assertEntries(t, svc.Effective("app", "https://github.com/o/app", cfg), []Entry{want[0], want[2]})
}

func TestEffectiveDiscoveryOffKeepsOnlyExplicit(t *testing.T) {
	hosts := &fakeHosts{
		entries:  []remotehost.Entry{entry("h_a", "Alpha", "pw")},
		behavior: map[string]func(context.Context) (Remote, error){"h_a": reachable(RemoteProject{ID: "app", Repo: "https://github.com/o/app"})},
		opens:    map[string]int{},
	}
	svc := newTestService(hosts, time.Second)
	svc.Refresh(context.Background())
	off := false
	cfg := domain.ProjectConfig{AggregateDiscovery: &off}
	if got := svc.Effective("app", "https://github.com/o/app", cfg); len(got) != 0 {
		t.Fatalf("discovery off returned %+v, want none", got)
	}
	cfg.AggregateProjects = []domain.AggregateProjectRef{{HostID: "h_a", ProjectID: "app"}}
	assertEntries(t, svc.Effective("app", "https://github.com/o/app", cfg), []Entry{{HostID: "h_a", HostLabel: "Alpha", ProjectID: "app", Source: SourceExplicit, Stale: false}})
}

func TestExplicitRowOfDiscoveredProjectAppearsOnceAsExplicit(t *testing.T) {
	hosts := &fakeHosts{
		entries:  []remotehost.Entry{entry("h_a", "Alpha", "pw")},
		behavior: map[string]func(context.Context) (Remote, error){"h_a": reachable(RemoteProject{ID: "app", Repo: "https://github.com/o/app"})},
		opens:    map[string]int{},
	}
	svc := newTestService(hosts, time.Second)
	svc.Refresh(context.Background())
	cfg := domain.ProjectConfig{AggregateProjects: []domain.AggregateProjectRef{{HostID: "h_a", ProjectID: "app"}}}
	assertEntries(t, svc.Effective("app", "https://github.com/o/app", cfg), []Entry{{HostID: "h_a", HostLabel: "Alpha", ProjectID: "app", Source: SourceExplicit, MatchedBy: MatchedByRepo}})
}

func TestUnreachableHostKeepsLastSetAsStale(t *testing.T) {
	hosts := &fakeHosts{
		entries:  []remotehost.Entry{entry("h_a", "Alpha", "pw")},
		behavior: map[string]func(context.Context) (Remote, error){"h_a": reachable(RemoteProject{ID: "app", Repo: "https://github.com/o/app"})},
		opens:    map[string]int{},
	}
	svc := newTestService(hosts, time.Second)
	svc.Refresh(context.Background())
	hosts.behavior["h_a"] = unreachable
	svc.Refresh(context.Background())
	assertEntries(t, svc.Effective("app", "https://github.com/o/app", domain.ProjectConfig{}), []Entry{{HostID: "h_a", HostLabel: "Alpha", ProjectID: "app", Source: SourceDiscovered, MatchedBy: MatchedByRepo, Stale: true}})
}

func TestRefusedHostShowsNothingAndIsNotRetriedUntilEntryChanges(t *testing.T) {
	hosts := &fakeHosts{
		entries:  []remotehost.Entry{entry("h_a", "Alpha", "old")},
		behavior: map[string]func(context.Context) (Remote, error){"h_a": refused},
		opens:    map[string]int{},
	}
	svc := newTestService(hosts, time.Second)
	svc.Refresh(context.Background())
	svc.Refresh(context.Background())
	if hosts.opens["h_a"] != 1 {
		t.Fatalf("refused host opened %d times, want 1", hosts.opens["h_a"])
	}
	if got := svc.Effective("app", "https://github.com/o/app", domain.ProjectConfig{}); len(got) != 0 {
		t.Fatalf("refused host returned %+v, want none", got)
	}

	hosts.mu.Lock()
	hosts.entries = []remotehost.Entry{entry("h_a", "Alpha", "new")}
	hosts.behavior["h_a"] = reachable(RemoteProject{ID: "app", Repo: "https://github.com/o/app"})
	hosts.mu.Unlock()
	svc.Refresh(context.Background())
	if hosts.opens["h_a"] != 2 {
		t.Fatalf("changed entry opened %d times, want 2", hosts.opens["h_a"])
	}
	if got := svc.Effective("app", "https://github.com/o/app", domain.ProjectConfig{}); len(got) != 1 {
		t.Fatalf("recovered host returned %+v, want one row", got)
	}
}

func TestSlowHostIsBoundedByBudgetAndDoesNotDelayOthers(t *testing.T) {
	hosts := &fakeHosts{
		entries: []remotehost.Entry{entry("h_slow", "Slow", "pw"), entry("h_fast", "Fast", "pw")},
		behavior: map[string]func(context.Context) (Remote, error){
			"h_slow": slow,
			"h_fast": reachable(RemoteProject{ID: "app", Repo: "https://github.com/o/app"}),
		},
		opens: map[string]int{},
	}
	svc := newTestService(hosts, 50*time.Millisecond)
	start := time.Now()
	svc.Refresh(context.Background())
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("pass took %s, want bounded by the host budget", elapsed)
	}
	got := svc.Effective("app", "https://github.com/o/app", domain.ProjectConfig{})
	assertEntries(t, got, []Entry{{HostID: "h_fast", HostLabel: "Fast", ProjectID: "app", Source: SourceDiscovered, MatchedBy: MatchedByRepo}})
}

func TestRemovedHostLeavesSetAndExclusionsAreKept(t *testing.T) {
	hosts := &fakeHosts{
		entries:  []remotehost.Entry{entry("h_a", "Alpha", "pw")},
		behavior: map[string]func(context.Context) (Remote, error){"h_a": reachable(RemoteProject{ID: "app", Repo: "https://github.com/o/app"})},
		opens:    map[string]int{},
	}
	svc := newTestService(hosts, time.Second)
	svc.Refresh(context.Background())
	cfg := domain.ProjectConfig{AggregateExcludes: []domain.AggregateProjectRef{{HostID: "h_a", ProjectID: "app"}}}
	hosts.mu.Lock()
	hosts.entries = nil
	hosts.mu.Unlock()
	svc.Refresh(context.Background())
	if got := svc.Effective("app", "https://github.com/o/app", cfg); len(got) != 0 {
		t.Fatalf("removed host returned %+v, want none", got)
	}
	hosts.mu.Lock()
	hosts.entries = []remotehost.Entry{entry("h_a", "Alpha", "pw")}
	hosts.mu.Unlock()
	svc.Refresh(context.Background())
	if got := svc.Effective("app", "https://github.com/o/app", cfg); len(got) != 0 {
		t.Fatalf("re-paired host ignored its exclusion: %+v", got)
	}
}

func assertEntries(t *testing.T, got, want []Entry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d entries %+v, want %d %+v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
