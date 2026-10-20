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

	// The same statement writes the root escalation and links the worker.
	var summaries, linked int
	var state, esc string
	if err := db.QueryRow(`SELECT count(*), COALESCE(MAX(state), ''), COALESCE(MAX(escalation_reason), '') FROM coordinator_summaries WHERE coordinator_session_id = 'ao-3'`).Scan(&summaries, &state, &esc); err != nil {
		t.Fatal(err)
	}
	if summaries != 1 || state != "blocked" || esc != "coordinator_terminated" {
		t.Fatalf("termination escalation = %d rows state=%q reason=%q", summaries, state, esc)
	}
	if err := db.QueryRow(`SELECT count(*) FROM coordinator_summary_workers WHERE worker_session_id = 'ao-4'`).Scan(&linked); err != nil || linked != 1 {
		t.Fatalf("escalation worker links = %d err=%v, want 1", linked, err)
	}
	// A repeated terminal write is not a transition: no second escalation.
	if _, err := db.Exec(`UPDATE sessions SET is_terminated = 1, updated_at = '2026-10-19T11:05:00Z' WHERE id = 'ao-3'`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM coordinator_summaries WHERE coordinator_session_id = 'ao-3'`).Scan(&summaries); err != nil || summaries != 1 {
		t.Fatalf("escalations after a repeated terminal write = %d err=%v, want 1", summaries, err)
	}
	// A restored coordinator that terminates again is a new event.
	if _, err := db.Exec(`UPDATE sessions SET is_terminated = 0, updated_at = '2026-10-19T12:00:00Z' WHERE id = 'ao-3'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE sessions SET is_terminated = 1, updated_at = '2026-10-19T13:00:00Z' WHERE id = 'ao-3'`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM coordinator_summaries WHERE coordinator_session_id = 'ao-3'`).Scan(&summaries); err != nil || summaries != 2 {
		t.Fatalf("escalations after a second termination = %d err=%v, want 2", summaries, err)
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

func TestCoordinatorWorkerLimitMigrationKeepsExistingCoordinatorsOnTheProjectLimit(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 217)
	if _, err := db.Exec(`
INSERT INTO projects (id, path, registered_at) VALUES ('ao', '/repo/ao', '2026-10-19T10:00:00Z');
INSERT INTO sessions (id, project_id, num, kind, coordinator_scope, harness, activity_last_at, created_at, updated_at)
VALUES ('ao-1', 'ao', 1, 'coordinator', 'auth area', 'claude-code', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z');
`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("migrate a database with a coordinator: %v", err)
	}
	var limit sql.NullInt64
	if err := db.QueryRow(`SELECT coordinator_worker_limit FROM sessions WHERE id = 'ao-1'`).Scan(&limit); err != nil || limit.Valid {
		t.Fatalf("existing coordinator limit = %v err=%v, want NULL", limit, err)
	}
	if _, err := db.Exec(`UPDATE sessions SET coordinator_worker_limit = 2 WHERE id = 'ao-1'`); err != nil {
		t.Fatalf("set a limit: %v", err)
	}
	if _, err := db.Exec(`UPDATE sessions SET coordinator_worker_limit = 0 WHERE id = 'ao-1'`); err == nil {
		t.Fatal("a limit below 1 must be rejected by the column check")
	}
	if _, err := db.Exec(`
INSERT INTO coordinator_limit_events (session_id, previous_limit, new_limit, actor_session_id, origin, created_at)
VALUES ('ao-1', NULL, 2, 'ao-root', 'ui', '2026-10-20T10:00:00Z')`); err != nil {
		t.Fatalf("audit row: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO coordinator_limit_events (session_id, previous_limit, new_limit, actor_session_id, origin, created_at)
VALUES ('ao-1', NULL, 2, 'ao-root', '', '2026-10-20T10:00:00Z')`); err == nil {
		t.Fatal("an empty origin must be rejected")
	}
}

func TestCoordinatorWorkerLimitMigrationDownDropsTheColumnAndAudit(t *testing.T) {
	db := openMigratedTestDB(t)
	if err := goose.DownTo(db, "migrations", 217); err != nil {
		t.Fatalf("down migration: %v", err)
	}
	if _, err := db.Exec(`SELECT coordinator_worker_limit FROM sessions`); err == nil {
		t.Fatal("the column survived the down migration")
	}
	if _, err := db.Exec(`SELECT 1 FROM coordinator_limit_events`); err == nil {
		t.Fatal("the audit table survived the down migration")
	}
	if err := migrate(db); err != nil {
		t.Fatalf("up again after down: %v", err)
	}
	if _, err := db.Exec(`SELECT coordinator_worker_limit FROM sessions`); err != nil {
		t.Fatalf("column missing after re-applying: %v", err)
	}
}

func TestCoordinatorHostSlotsMigrationKeepsAuditRowsAndAllowsNullLimits(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 218)
	if _, err := db.Exec(`
INSERT INTO projects (id, path, registered_at) VALUES ('ao', '/repo/ao', '2026-10-19T10:00:00Z');
INSERT INTO sessions (id, project_id, num, kind, coordinator_scope, harness, activity_last_at, created_at, updated_at)
VALUES ('ao-1', 'ao', 1, 'coordinator', 'auth area', 'claude-code', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z');
INSERT INTO coordinator_limit_events (session_id, previous_limit, new_limit, actor_session_id, origin, created_at)
VALUES ('ao-1', NULL, 2, 'ao-root', 'ui', '2026-10-20T10:00:00Z');
`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var newLimit sql.NullInt64
	var newSlots sql.NullString
	if err := db.QueryRow(`SELECT new_limit, new_host_slots FROM coordinator_limit_events WHERE session_id = 'ao-1'`).Scan(&newLimit, &newSlots); err != nil || newLimit.Int64 != 2 || newSlots.Valid {
		t.Fatalf("kept audit row = %v %v err=%v, want limit 2 and no slots", newLimit, newSlots, err)
	}
	if _, err := db.Exec(`
INSERT INTO coordinator_limit_events (session_id, previous_limit, new_limit, previous_host_slots, new_host_slots, actor_session_id, origin, created_at)
VALUES ('ao-1', 2, NULL, NULL, '[{"hostId":"host-a","slots":1}]', 'ao-root', 'cli', '2026-10-20T11:00:00Z')`); err != nil {
		t.Fatalf("a slot-only audit row with a NULL limit: %v", err)
	}
	if _, err := db.Exec(`UPDATE sessions SET coordinator_host_slots = 'not json' WHERE id = 'ao-1'`); err == nil {
		t.Fatal("invalid slot JSON must be rejected by the column check")
	}
	if _, err := db.Exec(`UPDATE sessions SET coordinator_host_slots = '[{"hostId":"host-a","slots":1}]' WHERE id = 'ao-1'`); err != nil {
		t.Fatalf("store slots: %v", err)
	}
}

func TestCoordinatorHostSlotsMigrationDownRestoresTheAuditTable(t *testing.T) {
	db := openMigratedTestDB(t)
	if _, err := db.Exec(`
INSERT INTO projects (id, path, registered_at) VALUES ('ao', '/repo/ao', '2026-10-19T10:00:00Z');
INSERT INTO sessions (id, project_id, num, kind, coordinator_scope, harness, activity_last_at, created_at, updated_at)
VALUES ('ao-1', 'ao', 1, 'coordinator', 'auth area', 'claude-code', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z');
INSERT INTO coordinator_limit_events (session_id, previous_limit, new_limit, new_host_slots, actor_session_id, origin, created_at)
VALUES ('ao-1', 3, NULL, '[{"hostId":"host-a","slots":1}]', 'ao-root', 'cli', '2026-10-20T11:00:00Z');
`); err != nil {
		t.Fatal(err)
	}
	if err := goose.DownTo(db, "migrations", 218); err != nil {
		t.Fatalf("down migration: %v", err)
	}
	if _, err := db.Exec(`SELECT coordinator_host_slots FROM sessions`); err == nil {
		t.Fatal("the column survived the down migration")
	}
	var newLimit int
	if err := db.QueryRow(`SELECT new_limit FROM coordinator_limit_events`).Scan(&newLimit); err != nil || newLimit != 3 {
		t.Fatalf("restored audit new_limit = %d err=%v, want the previous limit 3", newLimit, err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("up again after down: %v", err)
	}
}

func TestAgentProfilesMigrationKeepsExistingSessionsOnTheirAgent(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 218)
	if _, err := db.Exec(`
INSERT INTO projects (id, path, registered_at) VALUES ('ao', '/repo/ao', '2026-10-19T10:00:00Z');
INSERT INTO sessions (id, project_id, num, kind, coordinator_scope, harness, activity_last_at, created_at, updated_at)
VALUES ('ao-1', 'ao', 1, 'coordinator', 'auth area', 'claude-code', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z', '2026-10-19T10:00:00Z');
`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("migrate a database with a coordinator: %v", err)
	}
	var profile, selection sql.NullString
	if err := db.QueryRow(`SELECT agent_profile_id, coordinator_worker_agent FROM sessions WHERE id = 'ao-1'`).Scan(&profile, &selection); err != nil || profile.Valid || selection.Valid {
		t.Fatalf("existing session profile = %v selection = %v err=%v, want NULL", profile, selection, err)
	}
	if _, err := db.Exec(`UPDATE sessions SET agent_profile_id = 'a', coordinator_worker_agent = '{"mode":"fixed","profileId":"a"}' WHERE id = 'ao-1'`); err != nil {
		t.Fatalf("set profile: %v", err)
	}
	if _, err := db.Exec(`UPDATE sessions SET coordinator_worker_agent = 'not json' WHERE id = 'ao-1'`); err == nil {
		t.Fatal("a malformed selection must be rejected by the column check")
	}
	if _, err := db.Exec(`UPDATE sessions SET agent_profile_id = '' WHERE id = 'ao-1'`); err == nil {
		t.Fatal("an empty profile id must be rejected")
	}
	if _, err := db.Exec(`
INSERT INTO coordinator_agent_events (session_id, previous_selection, new_selection, actor_session_id, origin, created_at)
VALUES ('ao-1', NULL, '{"mode":"inherit"}', 'ao-root', 'cli', '2026-10-20T10:00:00Z')`); err != nil {
		t.Fatalf("audit row: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO coordinator_agent_events (session_id, previous_selection, new_selection, actor_session_id, origin, created_at)
VALUES ('ao-1', NULL, '{"mode":"inherit"}', 'ao-root', '', '2026-10-20T10:00:00Z')`); err == nil {
		t.Fatal("an empty origin must be rejected")
	}
}

func TestAgentProfilesMigrationDownDropsTheColumnsAndAudit(t *testing.T) {
	db := openMigratedTestDB(t)
	if err := goose.DownTo(db, "migrations", 218); err != nil {
		t.Fatalf("down migration: %v", err)
	}
	for _, q := range []string{`SELECT agent_profile_id FROM sessions`, `SELECT coordinator_worker_agent FROM sessions`, `SELECT 1 FROM coordinator_agent_events`} {
		if _, err := db.Exec(q); err == nil {
			t.Fatalf("%q still works after the down migration", q)
		}
	}
	if err := migrate(db); err != nil {
		t.Fatalf("up again after down: %v", err)
	}
	if _, err := db.Exec(`SELECT agent_profile_id, coordinator_worker_agent FROM sessions`); err != nil {
		t.Fatalf("columns missing after re-applying: %v", err)
	}
}
