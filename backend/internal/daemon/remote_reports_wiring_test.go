package daemon

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/remoteworker"
)

type wiringRemoteStore struct {
	mu        sync.Mutex
	links     []domain.RemoteWorkerLink
	listCalls int
}

func (s *wiringRemoteStore) GetSession(_ context.Context, id domain.SessionID) (domain.SessionRecord, bool, error) {
	return domain.SessionRecord{ID: id, ProjectID: "ultra-factory", Kind: domain.KindOrchestrator}, true, nil
}

func (s *wiringRemoteStore) ListRemoteWorkerLinks(context.Context) ([]domain.RemoteWorkerLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listCalls++
	return append([]domain.RemoteWorkerLink(nil), s.links...), nil
}

func (s *wiringRemoteStore) UpsertRemoteWorkerLink(_ context.Context, link domain.RemoteWorkerLink) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.links = append(s.links, link)
	return nil
}

func (s *wiringRemoteStore) GetRemoteWorkerLink(_ context.Context, hostID string, remoteSessionID domain.SessionID) (domain.RemoteWorkerLink, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, link := range s.links {
		if link.HostID == hostID && link.RemoteSessionID == remoteSessionID {
			return link, true, nil
		}
	}
	return domain.RemoteWorkerLink{}, false, nil
}

func (s *wiringRemoteStore) SetRemoteWorkerOwner(_ context.Context, hostID string, remoteSessionID domain.SessionID, owner *domain.SessionID, _ domain.OwnershipEventReason, _ time.Time) (domain.RemoteWorkerLink, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, link := range s.links {
		if link.HostID == hostID && link.RemoteSessionID == remoteSessionID {
			s.links[i].OwnerSessionID = owner
			return s.links[i], true, nil
		}
	}
	return domain.RemoteWorkerLink{}, false, nil
}

func (s *wiringRemoteStore) DeleteRemoteWorkerLink(context.Context, string, domain.SessionID) error {
	return nil
}

func (s *wiringRemoteStore) ListDeliveredRemoteReports(context.Context, string, domain.SessionID) (map[string]bool, error) {
	return nil, nil
}

func (s *wiringRemoteStore) RecordDeliveredRemoteReports(context.Context, string, domain.SessionID, []string, time.Time) error {
	return nil
}

func (s *wiringRemoteStore) RecordRemoteReportHistory(context.Context, string, domain.SessionID, []domain.ReportRecord) error {
	return nil
}

func (s *wiringRemoteStore) polls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listCalls
}

type wiringDeliverer struct{}

func (wiringDeliverer) DeliverExternal(context.Context, domain.ProjectID, []domain.ReportRecord) error {
	return nil
}

type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func TestRemoteReportsWiringSharesTheHostIdentityAndWakesOnLink(t *testing.T) {
	remotes := filepath.Join(t.TempDir(), "remotes.json")
	if err := os.WriteFile(remotes, []byte(`{"remotes":[{"label":"build-box","url":"https://127.0.0.1:1","password":"pw","hostId":"host-build"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AO_REMOTES_FILE", remotes)
	store := &wiringRemoteStore{links: []domain.RemoteWorkerLink{{HostID: "host-local", RemoteSessionID: "w-1"}}}
	var logs syncBuffer
	observer, links := newRemoteReports(store, wiringDeliverer{}, "host-local", slog.New(slog.NewTextHandler(&logs, nil)))

	// The link service refuses this machine.
	_, err := links.Register(context.Background(), remoteworker.RegisterInput{
		ParentSessionID: "ao-1", HostID: "host-local", RemoteSessionID: "w-2", RemoteProjectID: "p",
	})
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "REMOTE_HOST_IS_LOCAL" {
		t.Fatalf("self link err = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := observer.Start(ctx)
	waitPolls := func(n int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for store.polls() < n {
			if time.Now().After(deadline) {
				t.Fatalf("poll %d never happened", n)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitPolls(1)
	// The observer also knows this machine: the stored self link is skipped
	// before any saved host is looked up.
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), "linked host is this machine") {
		if time.Now().After(deadline) {
			t.Fatalf("observer did not get the host identity; logs = %s", logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	// A stored link wakes the observer at once instead of after the interval.
	before := store.polls()
	if _, err := links.Register(context.Background(), remoteworker.RegisterInput{
		ParentSessionID: "ao-1", HostID: "host-build", RemoteSessionID: "w-3", RemoteProjectID: "p",
	}); err != nil {
		t.Fatal(err)
	}
	waitPolls(before + 1)
	cancel()
	<-done
}
