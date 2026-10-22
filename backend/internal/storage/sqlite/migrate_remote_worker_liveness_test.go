package sqlite

import (
	"testing"

	"github.com/pressly/goose/v3"
)

// Migration 0227 adds the liveness columns and the removal memory. Rows written
// before it have never been read, so after a down and up round trip they must
// read no last state and no last-seen time.
func TestMigrationRemoteWorkerLinkLivenessRoundTrips(t *testing.T) {
	db := openMigratedTestDB(t)
	if _, err := db.Exec(`
INSERT INTO projects (id, path, registered_at) VALUES ('widgets', '/repo/widgets', '2026-10-22T09:00:00Z');
INSERT INTO remote_worker_links (host_id, remote_session_id, remote_project_id, project_id, host_label, display_name, created_at)
VALUES ('host-build', 'widgets-3', 'widgets', 'widgets', 'build-box', 'auth', '2026-10-22T09:00:00Z');
`); err != nil {
		t.Fatal(err)
	}

	if err := goose.DownTo(db, "migrations", 226); err != nil {
		t.Fatalf("down to 226: %v", err)
	}
	var columns, tables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('remote_worker_links') WHERE name IN ('last_seen_at', 'last_state')`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'remote_worker_link_removals'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if columns != 0 || tables != 0 {
		t.Fatalf("liveness columns = %d, removal table = %d after down; want none", columns, tables)
	}

	if err := goose.UpTo(db, "migrations", 227); err != nil {
		t.Fatalf("up to 227: %v", err)
	}
	var lastSeen *string
	var lastState string
	if err := db.QueryRow(`SELECT last_seen_at, last_state FROM remote_worker_links WHERE host_id = 'host-build' AND remote_session_id = 'widgets-3'`).Scan(&lastSeen, &lastState); err != nil {
		t.Fatal(err)
	}
	if lastSeen != nil || lastState != "" {
		t.Fatalf("liveness = %v %q, want unread for a pre-existing row", lastSeen, lastState)
	}
}
