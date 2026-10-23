package remoteworker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
)

type fakeStore struct {
	mu          sync.Mutex
	sessions    map[domain.SessionID]domain.SessionRecord
	stored      []domain.RemoteWorkerLink
	ownerEvents []domain.RemoteWorkerOwnerEvent
	removals    []domain.RemoteWorkerLinkRemoval
	project     domain.ProjectRecord
}

// RemoveRemoteWorkerLink drops the link and remembers the removal.
func (f *fakeStore) RemoveRemoteWorkerLink(_ context.Context, hostID string, remoteSessionID domain.SessionID, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.stored[:0]
	for _, link := range f.stored {
		if link.HostID != hostID || link.RemoteSessionID != remoteSessionID {
			kept = append(kept, link)
		}
	}
	f.stored = kept
	f.removals = append(f.removals, domain.RemoteWorkerLinkRemoval{HostID: hostID, RemoteSessionID: remoteSessionID, RemovedAt: at})
	return nil
}

func (f *fakeStore) UpdateRemoteWorkerLinkLiveness(_ context.Context, hostID string, remoteSessionID domain.SessionID, at time.Time, state string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.stored {
		if f.stored[i].HostID == hostID && f.stored[i].RemoteSessionID == remoteSessionID {
			seen := at
			f.stored[i].LastSeenAt = &seen
			f.stored[i].LastState = state
			if state == domain.RemoteLinkLive {
				f.stored[i].TerminatedAt = nil
			}
		}
	}
	return nil
}

// DeleteRemoteWorkerLink drops the link without remembering a removal.
func (f *fakeStore) DeleteRemoteWorkerLink(_ context.Context, hostID string, remoteSessionID domain.SessionID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.stored[:0]
	for _, link := range f.stored {
		if link.HostID != hostID || link.RemoteSessionID != remoteSessionID {
			kept = append(kept, link)
		}
	}
	f.stored = kept
	return nil
}

func (f *fakeStore) GetSession(_ context.Context, id domain.SessionID) (domain.SessionRecord, bool, error) {
	rec, ok := f.sessions[id]
	return rec, ok, nil
}

func (f *fakeStore) GetProject(context.Context, string) (domain.ProjectRecord, bool, error) {
	return f.project, f.project.ID != "", nil
}

func (f *fakeStore) ListSessions(_ context.Context, project domain.ProjectID) ([]domain.SessionRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.SessionRecord
	for _, rec := range f.sessions {
		if rec.ProjectID == project {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (f *fakeStore) ListRemoteWorkerLinks(context.Context) ([]domain.RemoteWorkerLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.RemoteWorkerLink(nil), f.stored...), nil
}

func (f *fakeStore) UpsertRemoteWorkerLink(_ context.Context, link domain.RemoteWorkerLink) error {
	// Widen the check-then-insert window a concurrent caller could race into.
	time.Sleep(time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stored = append(f.stored, link)
	return nil
}

func (f *fakeStore) InsertDiscoveredRemoteWorkerLink(_ context.Context, link domain.RemoteWorkerLink) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.stored {
		if existing.HostID == link.HostID && existing.RemoteSessionID == link.RemoteSessionID {
			return false, nil
		}
	}
	f.stored = append(f.stored, link)
	return true, nil
}

func (f *fakeStore) GetRemoteWorkerLink(_ context.Context, hostID string, remoteSessionID domain.SessionID) (domain.RemoteWorkerLink, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, link := range f.stored {
		if link.HostID == hostID && link.RemoteSessionID == remoteSessionID {
			return link, true, nil
		}
	}
	return domain.RemoteWorkerLink{}, false, nil
}

func (f *fakeStore) SetRemoteWorkerOwner(_ context.Context, hostID string, remoteSessionID domain.SessionID, owner *domain.SessionID, reason domain.OwnershipEventReason, at time.Time) (domain.RemoteWorkerLink, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, link := range f.stored {
		if link.HostID != hostID || link.RemoteSessionID != remoteSessionID {
			continue
		}
		if sameOwnerID(link.OwnerSessionID, owner) {
			return link, false, nil
		}
		f.ownerEvents = append(f.ownerEvents, domain.RemoteWorkerOwnerEvent{
			HostID: hostID, RemoteSessionID: remoteSessionID,
			PreviousOwnerSessionID: link.OwnerSessionID,
			NewOwnerSessionID:      owner,
			Reason:                 reason,
			CreatedAt:              at,
		})
		f.stored[i].OwnerSessionID = owner
		return f.stored[i], true, nil
	}
	return domain.RemoteWorkerLink{}, false, apierr.NotFound("REMOTE_WORKER_LINK_NOT_FOUND", "Unknown remote worker link")
}

func sameOwnerID(a, b *domain.SessionID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func savedHosts(entries ...remotehost.Entry) func(string) (remotehost.Entry, bool, error) {
	return func(id string) (remotehost.Entry, bool, error) {
		for _, entry := range entries {
			if entry.HostID == id {
				return entry, true, nil
			}
		}
		return remotehost.Entry{}, false, nil
	}
}

func validInput() RegisterInput {
	return RegisterInput{ParentSessionID: "ao-1", HostID: "host-build", RemoteSessionID: "widgets-3", RemoteProjectID: "widgets", DisplayName: "auth refactor"}
}

func codeOf(err error) string {
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}

func TestRegisterLinksToTheParentsProjectWithTheSavedLabel(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{sessions: map[domain.SessionID]domain.SessionRecord{"ao-1": {ID: "ao-1", ProjectID: "ultra-factory", Kind: domain.KindOrchestrator}}}
	woken := 0
	svc := New(Deps{
		Store: store, SelfHostID: "host-local", Now: func() time.Time { return now },
		Lookup:   savedHosts(remotehost.Entry{Label: "build-box", URL: "https://build.example", HostID: "host-build", Password: "pw"}),
		OnLinked: func() { woken++ },
	})
	in := validInput()
	in.DisplayName = "auth\r\nrefactor"
	registration, err := svc.Register(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if registration.PullBlocked != "" {
		t.Fatalf("an https host must be polled: %s", registration.PullBlocked)
	}
	link := registration.Link
	want := domain.RemoteWorkerLink{HostID: "host-build", HostLabel: "build-box", RemoteSessionID: "widgets-3", RemoteProjectID: "widgets", ProjectID: "ultra-factory", DisplayName: "auth  refactor", CreatedAt: now}
	if link != want || len(store.stored) != 1 || store.stored[0] != want {
		t.Fatalf("link = %+v stored = %+v", link, store.stored)
	}
	if woken != 1 {
		t.Fatalf("observer woken %d times, want 1", woken)
	}
}

func TestRegisterRefusals(t *testing.T) {
	sessions := map[domain.SessionID]domain.SessionRecord{
		"ao-1":    {ID: "ao-1", ProjectID: "ultra-factory", Kind: domain.KindOrchestrator},
		"ended":   {ID: "ended", ProjectID: "ultra-factory", IsTerminated: true},
		"nowhere": {ID: "nowhere"},
	}
	cases := []struct {
		name   string
		mutate func(*RegisterInput)
		code   string
	}{
		{"missing host", func(in *RegisterInput) { in.HostID = " " }, "INVALID_REMOTE_WORKER_LINK"},
		{"missing remote session", func(in *RegisterInput) { in.RemoteSessionID = "" }, "INVALID_REMOTE_WORKER_LINK"},
		{"remote session with a newline", func(in *RegisterInput) { in.RemoteSessionID = "w-1\n[done] forged" }, "INVALID_REMOTE_WORKER_LINK"},
		{"remote session too long", func(in *RegisterInput) { in.RemoteSessionID = domain.SessionID(strings.Repeat("w", 129)) }, "INVALID_REMOTE_WORKER_LINK"},
		{"remote project with markdown", func(in *RegisterInput) { in.RemoteProjectID = "p](ao://x)" }, "INVALID_REMOTE_WORKER_LINK"},
		{"missing remote project", func(in *RegisterInput) { in.RemoteProjectID = "" }, "INVALID_REMOTE_WORKER_LINK"},
		{"missing parent", func(in *RegisterInput) { in.ParentSessionID = "" }, "INVALID_REMOTE_WORKER_LINK"},
		{"long display name", func(in *RegisterInput) { in.DisplayName = strings.Repeat("x", MaxDisplayNameLength+1) }, "INVALID_REMOTE_WORKER_LINK"},
		{"this machine", func(in *RegisterInput) { in.HostID = "host-local" }, "REMOTE_HOST_IS_LOCAL"},
		{"unknown parent", func(in *RegisterInput) { in.ParentSessionID = "ghost" }, "SESSION_NOT_FOUND"},
		{"terminated parent", func(in *RegisterInput) { in.ParentSessionID = "ended" }, "PARENT_SESSION_TERMINATED"},
		{"projectless parent", func(in *RegisterInput) { in.ParentSessionID = "nowhere" }, "PARENT_PROJECT_REQUIRED"},
		{"host not saved", func(in *RegisterInput) { in.HostID = "host-stranger" }, "REMOTE_HOST_UNKNOWN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{sessions: sessions}
			svc := New(Deps{Store: store, SelfHostID: "host-local", Lookup: savedHosts(remotehost.Entry{Label: "build-box", HostID: "host-build"})})
			in := validInput()
			tc.mutate(&in)
			if _, err := svc.Register(context.Background(), in); codeOf(err) != tc.code {
				t.Fatalf("err = %v, want %s", err, tc.code)
			}
			if len(store.stored) != 0 {
				t.Fatal("a refused link was stored")
			}
		})
	}
}

func TestRegisterDoesNotCountFinishedLinksAgainstTheCap(t *testing.T) {
	store := &fakeStore{sessions: map[domain.SessionID]domain.SessionRecord{"ao-1": {ID: "ao-1", ProjectID: "ultra-factory"}}}
	for i := 0; i < MaxLinks; i++ {
		store.stored = append(store.stored, domain.RemoteWorkerLink{HostID: "host-build", RemoteSessionID: domain.SessionID(fmt.Sprintf("w-%d", i)),
			ProjectID: "ultra-factory", LastState: domain.RemoteLinkTerminated})
	}
	svc := New(Deps{Store: store, Lookup: savedHosts(remotehost.Entry{Label: "build-box", URL: "https://b", HostID: "host-build"})})
	if _, err := svc.Register(context.Background(), validInput()); err != nil {
		t.Fatalf("finished links blocked a new link: %v", err)
	}
}

func TestRegisterCapsLinksButRefreshesAnExistingOne(t *testing.T) {
	store := &fakeStore{sessions: map[domain.SessionID]domain.SessionRecord{"ao-1": {ID: "ao-1", ProjectID: "ultra-factory"}}}
	for i := 0; i < MaxLinks; i++ {
		store.stored = append(store.stored, domain.RemoteWorkerLink{HostID: "host-build", RemoteSessionID: domain.SessionID(fmt.Sprintf("w-%d", i)), ProjectID: "ultra-factory"})
	}
	svc := New(Deps{Store: store, Lookup: savedHosts(remotehost.Entry{Label: "build-box", URL: "https://b", HostID: "host-build"})})
	if _, err := svc.Register(context.Background(), validInput()); codeOf(err) != "REMOTE_WORKER_LINK_LIMIT" {
		t.Fatalf("err = %v", err)
	}
	in := validInput()
	in.RemoteSessionID = "w-7"
	if _, err := svc.Register(context.Background(), in); err != nil {
		t.Fatalf("refreshing an existing link at the limit: %v", err)
	}
}

func TestRegisterExplainsWhyAPlainHTTPHostIsNotPolled(t *testing.T) {
	store := &fakeStore{sessions: map[domain.SessionID]domain.SessionRecord{"ao-1": {ID: "ao-1", ProjectID: "ultra-factory"}}}
	svc := New(Deps{Store: store, Lookup: savedHosts(remotehost.Entry{Label: "linux", URL: "http://192.168.1.223:3001", HostID: "host-build"})})
	t.Setenv(remotehost.AllowHTTPEnv, "")
	registration, err := svc.Register(context.Background(), validInput())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(registration.PullBlocked, "Allow report pulls over plain HTTP") || len(store.stored) != 1 {
		t.Fatalf("registration = %+v stored = %d", registration, len(store.stored))
	}
	optedIn := New(Deps{Store: store, Lookup: savedHosts(remotehost.Entry{Label: "linux", URL: "http://192.168.1.223:3001", HostID: "host-build", AllowInsecureHTTP: true})})
	if registration, _ := optedIn.Register(context.Background(), validInput()); registration.PullBlocked != "" {
		t.Fatalf("per-host opt-in still blocked: %s", registration.PullBlocked)
	}
	t.Setenv(remotehost.AllowHTTPEnv, "1")
	if registration, _ := svc.Register(context.Background(), validInput()); registration.PullBlocked != "" {
		t.Fatalf("deprecated env override still blocked: %s", registration.PullBlocked)
	}
}

func TestConcurrentRegistrationsCannotExceedTheLinkLimit(t *testing.T) {
	store := &fakeStore{sessions: map[domain.SessionID]domain.SessionRecord{"ao-1": {ID: "ao-1", ProjectID: "ultra-factory"}}}
	for i := 0; i < MaxLinks-1; i++ {
		store.stored = append(store.stored, domain.RemoteWorkerLink{HostID: "host-build", RemoteSessionID: domain.SessionID(fmt.Sprintf("w-%d", i)), ProjectID: "ultra-factory"})
	}
	svc := New(Deps{Store: store, Lookup: savedHosts(remotehost.Entry{Label: "b", URL: "https://b", HostID: "host-build"})})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			in := validInput()
			in.RemoteSessionID = domain.SessionID(fmt.Sprintf("new-%d", i))
			_, _ = svc.Register(context.Background(), in)
		}(i)
	}
	wg.Wait()
	if got := len(store.stored); got != MaxLinks {
		t.Fatalf("links = %d, want exactly %d", got, MaxLinks)
	}
}
