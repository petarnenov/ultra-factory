package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

type fakeRemoteSpawned struct {
	marked  []domain.SessionID
	active  []domain.SessionID
	markErr error
	listErr error
}

func (f *fakeRemoteSpawned) MarkRemoteSpawned(_ context.Context, id domain.SessionID, _ time.Time) error {
	if f.markErr != nil {
		return f.markErr
	}
	f.marked = append(f.marked, id)
	return nil
}

func (f *fakeRemoteSpawned) ListActiveRemoteSpawnedSessions(context.Context) ([]domain.SessionID, error) {
	return f.active, f.listErr
}

func wantRemoteWorkersActive(t *testing.T, err error) {
	t.Helper()
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "REMOTE_WORKERS_ACTIVE" {
		t.Fatalf("err = %v, want REMOTE_WORKERS_ACTIVE", err)
	}
}

func TestRemoteSpawnedWorkerIsMarked(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer"}
	fc := &fakeCommander{spawnRecord: domain.SessionRecord{ID: "mer-3", ProjectID: "mer", Kind: domain.KindWorker}}
	remote := &fakeRemoteSpawned{}
	svc := &Service{manager: fc, store: st, remoteSpawned: remote}

	if _, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, RemoteSpawned: true}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if len(remote.marked) != 1 || remote.marked[0] != "mer-3" {
		t.Fatalf("marked = %v, want mer-3", remote.marked)
	}
}

func TestLocalWorkerIsNotMarked(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer"}
	fc := &fakeCommander{spawnRecord: domain.SessionRecord{ID: "mer-3", ProjectID: "mer", Kind: domain.KindWorker}}
	remote := &fakeRemoteSpawned{}
	svc := &Service{manager: fc, store: st, remoteSpawned: remote}

	if _, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if len(remote.marked) != 0 {
		t.Fatalf("marked = %v, want none for a local spawn", remote.marked)
	}
}

func TestRemoteSpawnedWorkerMarkFailureFailsTheSpawn(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer"}
	fc := &fakeCommander{spawnRecord: domain.SessionRecord{ID: "mer-3", ProjectID: "mer", Kind: domain.KindWorker}}
	svc := &Service{manager: fc, store: st, remoteSpawned: &fakeRemoteSpawned{markErr: errors.New("disk full")}}

	_, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, RemoteSpawned: true})
	if err == nil || err.Error() != "disk full" {
		t.Fatalf("err = %v, want the mark failure", err)
	}
}

func TestRemoteSpawnRejectsOrchestrators(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer"}
	fc := &fakeCommander{}
	svc := &Service{manager: fc, store: st, remoteSpawned: &fakeRemoteSpawned{}}

	_, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindOrchestrator, RemoteSpawned: true})
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "REMOTE_SPAWN_WORKER_REQUIRED" {
		t.Fatalf("err = %v, want REMOTE_SPAWN_WORKER_REQUIRED", err)
	}
	if fc.spawned {
		t.Fatal("an invalid remote spawn must not launch")
	}
}

func TestOrchestratorSpawnRefusedWhileRemoteWorkerActive(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer"}
	fc := &fakeCommander{}
	svc := &Service{manager: fc, store: st, remoteSpawned: &fakeRemoteSpawned{active: []domain.SessionID{"mer-3"}}}

	_, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindOrchestrator})
	wantRemoteWorkersActive(t, err)
	if fc.spawned {
		t.Fatal("orchestrator must not launch while a remote worker is active")
	}
}

func TestSpawnOrchestratorRefusedWhileRemoteWorkerActive(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer"}
	st.sessions["mer-1"] = domain.SessionRecord{ID: "mer-1", ProjectID: "mer", Kind: domain.KindOrchestrator}
	fc := &fakeCommander{}
	svc := &Service{manager: fc, store: st, remoteSpawned: &fakeRemoteSpawned{active: []domain.SessionID{"mer-3"}}}

	_, err := svc.SpawnOrchestrator(context.Background(), "mer", true, "", "")
	wantRemoteWorkersActive(t, err)
	if fc.spawned || len(fc.retired) != 0 {
		t.Fatalf("refusal must come before any retirement or launch: spawned=%v retired=%v", fc.spawned, fc.retired)
	}
}

func TestSpawnOrchestratorAllowedWithoutActiveRemoteWorkers(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer"}
	fc := &fakeCommander{}
	svc := &Service{manager: fc, store: st, remoteSpawned: &fakeRemoteSpawned{}}

	if _, err := svc.SpawnOrchestrator(context.Background(), "mer", false, "", ""); err != nil {
		t.Fatalf("SpawnOrchestrator: %v", err)
	}
	if !fc.spawned {
		t.Fatal("orchestrator should launch when no remote worker is active")
	}
}

func TestRestoreOrchestratorRefusedWhileRemoteWorkerActive(t *testing.T) {
	st := newFakeStore()
	st.sessions["mer-1"] = domain.SessionRecord{ID: "mer-1", ProjectID: "mer", Kind: domain.KindOrchestrator, IsTerminated: true}
	fc := &fakeCommander{restoreErr: errors.New("restore must not run")}
	svc := &Service{manager: fc, store: st, remoteSpawned: &fakeRemoteSpawned{active: []domain.SessionID{"mer-3"}}}

	_, err := svc.Restore(context.Background(), "mer-1")
	wantRemoteWorkersActive(t, err)
}

func TestRestoreWorkerAllowedWhileRemoteWorkerActive(t *testing.T) {
	st := newFakeStore()
	rec := domain.SessionRecord{ID: "mer-2", ProjectID: "mer", Kind: domain.KindWorker}
	st.sessions["mer-2"] = rec
	fc := &fakeCommander{restoreResult: sessionmanager.RestoreResult{Session: rec}}
	svc := &Service{manager: fc, store: st, remoteSpawned: &fakeRemoteSpawned{active: []domain.SessionID{"mer-3"}}}

	if _, err := svc.Restore(context.Background(), "mer-2"); err != nil {
		t.Fatalf("Restore worker: %v", err)
	}
}

func TestRemoteWorkerRefusedWhileLocalOrchestratorActive(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer"}
	st.sessions["other-1"] = domain.SessionRecord{ID: "other-1", ProjectID: "other", Kind: domain.KindOrchestrator}
	fc := &fakeCommander{}
	remote := &fakeRemoteSpawned{}
	svc := &Service{manager: fc, store: st, remoteSpawned: remote}

	_, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, RemoteSpawned: true})
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "LOCAL_ORCHESTRATOR_ACTIVE" {
		t.Fatalf("err = %v, want LOCAL_ORCHESTRATOR_ACTIVE", err)
	}
	if fc.spawned || len(remote.marked) != 0 {
		t.Fatalf("refused remote worker must not launch or be marked: spawned=%v marked=%v", fc.spawned, remote.marked)
	}
}

func TestRemoteWorkerAllowedBesideTerminatedOrchestrator(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer"}
	st.sessions["mer-1"] = domain.SessionRecord{ID: "mer-1", ProjectID: "mer", Kind: domain.KindOrchestrator, IsTerminated: true}
	fc := &fakeCommander{spawnRecord: domain.SessionRecord{ID: "mer-3", ProjectID: "mer", Kind: domain.KindWorker}}
	svc := &Service{manager: fc, store: st, remoteSpawned: &fakeRemoteSpawned{}}

	if _, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, RemoteSpawned: true}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
}

func TestLocalWorkerAllowedBesideLocalOrchestrator(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer"}
	st.sessions["mer-1"] = domain.SessionRecord{ID: "mer-1", ProjectID: "mer", Kind: domain.KindOrchestrator}
	fc := &fakeCommander{spawnRecord: domain.SessionRecord{ID: "mer-3", ProjectID: "mer", Kind: domain.KindWorker}}
	svc := &Service{manager: fc, store: st, remoteSpawned: &fakeRemoteSpawned{}}

	if _, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
}
