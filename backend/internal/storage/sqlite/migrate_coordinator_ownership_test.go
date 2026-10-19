package sqlite

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

func seedCoordinatorOwnershipLegacyRows(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`
INSERT INTO projects (id, path, registered_at) VALUES
    ('ao', '/repo/ao', '2026-10-19T10:00:00Z'),
    ('other', '/repo/other', '2026-10-19T10:00:00Z');
INSERT INTO sessions (id, project_id, num, kind, harness, activity_last_at, created_at, updated_at) VALUES
    ('ao-1', 'ao', 1, 'orchestrator', 'claude-code', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z'),
    ('ao-2', 'ao', 2, 'worker', 'claude-code', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z');
INSERT INTO remote_worker_links (host_id, remote_session_id, remote_project_id, project_id, host_label, display_name, created_at)
VALUES ('host-build', 'widgets-3', 'widgets', 'ao', 'build-box', 'remote task', '2026-10-19T10:00:00Z');
`); err != nil {
		t.Fatalf("seed pre-coordinator rows: %v", err)
	}
}

func TestCoordinatorOwnershipMigrationKeepsExistingWorkersRootOwned(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 206)
	seedCoordinatorOwnershipLegacyRows(t, db)

	if err := migrate(db); err != nil {
		t.Fatalf("migrate a populated 0.17.1 database: %v", err)
	}

	var owner sql.NullString
	var scope string
	if err := db.QueryRow(`SELECT owner_session_id, coordinator_scope FROM sessions WHERE id = 'ao-2'`).Scan(&owner, &scope); err != nil {
		t.Fatal(err)
	}
	if owner.Valid || scope != "" {
		t.Fatalf("existing worker = owner %v scope %q, want root-owned", owner, scope)
	}
	if err := db.QueryRow(`SELECT owner_session_id FROM remote_worker_links WHERE host_id = 'host-build'`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner.Valid {
		t.Fatalf("existing remote link owner = %v, want root-owned", owner)
	}

	// Existing rows remain valid, and the new kind and owner write paths work.
	if _, err := db.Exec(`
INSERT INTO sessions (id, project_id, num, kind, coordinator_scope, harness, activity_last_at, created_at, updated_at)
VALUES ('ao-3', 'ao', 3, 'coordinator', 'auth area', 'claude-code', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z');
INSERT INTO sessions (id, project_id, num, kind, harness, owner_session_id, activity_last_at, created_at, updated_at)
VALUES ('ao-4', 'ao', 4, 'worker', 'claude-code', 'ao-3', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z');
`); err != nil {
		t.Fatalf("insert coordinator-owned worker: %v", err)
	}
	if err := db.QueryRow(`SELECT owner_session_id FROM sessions WHERE id = 'ao-4'`).Scan(&owner); err != nil || !owner.Valid || owner.String != "ao-3" {
		t.Fatalf("worker owner = %v err=%v, want ao-3", owner, err)
	}
	// Direct writers hit the same scope rules as the service/store path.
	for _, tc := range []struct {
		id    string
		scope string
		want  string
	}{
		{"ao-scope-untrimmed", "  auth  ", "must be trimmed"},
		{"ao-scope-long", strings.Repeat("x", 201), "at most 200"},
		{"ao-scope-control", "auth\x1barea", "control characters"},
		{"ao-scope-nul", "auth\x00area", "control characters"},
		{"ao-scope-c1", "auth\u0085area", "control characters"},
	} {
		if _, err := db.Exec(`
INSERT INTO sessions (id, project_id, num, kind, coordinator_scope, harness, activity_last_at, created_at, updated_at)
VALUES (?, 'ao', (SELECT COALESCE(MAX(num), 0) + 1 FROM sessions WHERE project_id = 'ao'), 'coordinator', ?, 'claude-code', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z')
`, tc.id, tc.scope); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("scope %q insert err = %v, want %q", tc.scope, err, tc.want)
		}
	}

	if _, err := db.Exec(`UPDATE remote_worker_links SET owner_session_id = 'ao-3' WHERE host_id = 'host-build'`); err != nil {
		t.Fatalf("own the existing remote link: %v", err)
	}
	if _, err := db.Exec(`UPDATE remote_worker_links SET project_id = 'other' WHERE host_id = 'host-build'`); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("remote project move err = %v", err)
	}
	if _, err := db.Exec(`UPDATE remote_worker_links SET owner_session_id = 'ao-1' WHERE host_id = 'host-build'`); err == nil || !strings.Contains(err.Error(), "live coordinator") {
		t.Fatalf("invalid remote owner err = %v", err)
	}

	// The trigger rejects an invalid owner even without the service.
	if _, err := db.Exec(`
INSERT INTO sessions (id, project_id, num, kind, harness, owner_session_id, activity_last_at, created_at, updated_at)
VALUES ('ao-5', 'ao', 5, 'worker', 'claude-code', 'ao-1', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z')
`); err == nil || !strings.Contains(err.Error(), "live coordinator") {
		t.Fatalf("root-owned-by-orchestrator insert err = %v", err)
	}
	if _, err := db.Exec(`UPDATE sessions SET owner_session_id = 'ao-1' WHERE id = 'ao-4'`); err == nil || !strings.Contains(err.Error(), "live coordinator") {
		t.Fatalf("invalid owner update err = %v", err)
	}
	if _, err := db.Exec(`UPDATE sessions SET coordinator_scope = 'another area' WHERE id = 'ao-3'`); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("scope update err = %v", err)
	}

	// Termination releases the worker and audits the handback in one statement.
	if _, err := db.Exec(`UPDATE sessions SET is_terminated = 1, updated_at = '2026-10-19T11:00:00Z' WHERE id = 'ao-3'`); err != nil {
		t.Fatalf("terminate coordinator: %v", err)
	}
	if err := db.QueryRow(`SELECT owner_session_id FROM sessions WHERE id = 'ao-4'`).Scan(&owner); err != nil || owner.Valid {
		t.Fatalf("worker owner after termination = %v err=%v, want NULL", owner, err)
	}
	var reason string
	if err := db.QueryRow(`SELECT reason FROM session_owner_events WHERE session_id = 'ao-4' ORDER BY id DESC LIMIT 1`).Scan(&reason); err != nil || reason != "coordinator_terminated" {
		t.Fatalf("termination audit reason = %q err=%v", reason, err)
	}
	if err := db.QueryRow(`SELECT owner_session_id FROM remote_worker_links WHERE host_id = 'host-build'`).Scan(&owner); err != nil || owner.Valid {
		t.Fatalf("remote owner after termination = %v err=%v, want NULL", owner, err)
	}
	if err := db.QueryRow(`SELECT reason FROM remote_worker_owner_events WHERE host_id = 'host-build' ORDER BY id DESC LIMIT 1`).Scan(&reason); err != nil || reason != "coordinator_terminated" {
		t.Fatalf("remote termination audit reason = %q err=%v", reason, err)
	}
}

func TestCoordinatorKindMigrationDownRefusesCoordinatorRows(t *testing.T) {
	db := openMigratedTestDB(t)
	if _, err := db.Exec(`
INSERT INTO projects (id, path, registered_at) VALUES ('ao', '/repo/ao', '2026-10-19T10:00:00Z');
INSERT INTO sessions (id, project_id, num, kind, coordinator_scope, harness, activity_last_at, created_at, updated_at)
VALUES ('ao-1', 'ao', 1, 'coordinator', 'auth area', 'claude-code', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z');
`); err != nil {
		t.Fatal(err)
	}
	err := goose.DownTo(db, "migrations", 206)
	if err == nil || !strings.Contains(err.Error(), "constraint failed") {
		t.Fatalf("down migration err = %v, want the coordinator guard to abort", err)
	}
}

func TestCoordinatorKindMigrationDownNarrowsAfterCoordinatorsAreGone(t *testing.T) {
	db := openMigratedTestDB(t)
	if _, err := db.Exec(`INSERT INTO projects (id, path, registered_at) VALUES ('ao', '/repo/ao', '2026-10-19T10:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := goose.DownTo(db, "migrations", 206); err != nil {
		t.Fatalf("down migration with no coordinator rows: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO sessions (id, project_id, num, kind, harness, activity_last_at, created_at, updated_at)
VALUES ('ao-2', 'ao', 2, 'coordinator', 'claude-code', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z')
`); err == nil {
		t.Fatal("coordinator kind remained allowed after the down migration")
	}
}

func TestReconcileSessionKindConstraintRepairsBurnedMigration(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 206)
	var before string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'sessions'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(before, "'coordinator'") {
		t.Fatal("206 snapshot unexpectedly allows coordinator")
	}
	if err := reconcileSessionKindConstraint(db); err != nil {
		t.Fatalf("repair kind constraint: %v", err)
	}
	var after string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'sessions'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(after, "'coordinator'") {
		t.Fatalf("kind constraint after repair = %s", after)
	}
	if err := reconcileSessionKindConstraint(db); err != nil {
		t.Fatalf("repeat repair: %v", err)
	}
}
