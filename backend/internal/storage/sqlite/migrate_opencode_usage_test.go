package sqlite

import (
	"database/sql"
	"testing"
)

// TestMigrateAllowsOpencodeV2Usage proves the widened usage constraints accept
// the OpenCode 2 identity: a binding and a snapshot source.
func TestMigrateAllowsOpencodeV2Usage(t *testing.T) {
	db := openMigratedTestDB(t)
	seedOpencodeV2Usage(t, db)
	assertTableSQLContains(t, db, "usage_bindings", "'opencode-v2'")
	assertTableSQLContains(t, db, "usage_sources", "'opencode_v2_session'")
}

// TestMigrateRejectsUnknownUsageIdentity keeps the widened constraints honest:
// a harness or source kind AO does not certify is still refused.
func TestMigrateRejectsUnknownUsageIdentity(t *testing.T) {
	db := openMigratedTestDB(t)
	seedOpencodeV2Usage(t, db)
	if _, err := db.Exec(`INSERT INTO usage_bindings (session_id, harness, native_root_id, state, updated_at)
		VALUES ('oc-1', 'not-a-harness', 'native-2', 'active', CURRENT_TIMESTAMP)`); err == nil {
		t.Fatal("an uncertified harness must not bind a usage source")
	}
	if _, err := db.Exec(`INSERT INTO usage_sources (binding_id, kind, artifact_path, state, updated_at)
		VALUES ((SELECT id FROM usage_bindings WHERE session_id = 'oc-1'), 'not_a_kind', '/tmp/x', 'active', CURRENT_TIMESTAMP)`); err == nil {
		t.Fatal("an uncertified source kind must not be stored")
	}
}

func seedOpencodeV2Usage(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO projects (id, path, registered_at, config)
		VALUES ('oc', '/repo/oc', CURRENT_TIMESTAMP, '{}')`); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (id, project_id, num, kind, harness, activity_state, activity_last_at, is_terminated, created_at, updated_at)
		VALUES ('oc-1', 'oc', 1, 'worker', 'opencode-v2', 'exited', CURRENT_TIMESTAMP, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO usage_bindings (session_id, harness, native_root_id, state, updated_at)
		VALUES ('oc-1', 'opencode-v2', 'ses_opencode', 'finalizing', CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("seed opencode-v2 binding: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO usage_sources (binding_id, kind, native_session_id, artifact_path, state, updated_at)
		VALUES ((SELECT id FROM usage_bindings WHERE session_id = 'oc-1'), 'opencode_v2_session', 'ses_opencode',
		        '/home/u/.local/share/opencode-v2-home/opencode/opencode.db', 'active', CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("seed opencode-v2 source: %v", err)
	}
}
