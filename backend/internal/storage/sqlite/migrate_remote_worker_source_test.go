package sqlite

import (
	"testing"

	"github.com/pressly/goose/v3"
)

// Migration 0225 adds remote_worker_links.source. Rows written before it are
// spawn-created, so after a down and up round trip they must read "spawn".
func TestMigrationRemoteWorkerLinkSourceRoundTrips(t *testing.T) {
	db := openMigratedTestDB(t)
	if _, err := db.Exec(`
INSERT INTO projects (id, path, registered_at) VALUES ('widgets', '/repo/widgets', '2026-10-21T09:00:00Z');
INSERT INTO remote_worker_links (host_id, remote_session_id, remote_project_id, project_id, host_label, display_name, created_at)
VALUES ('host-build', 'widgets-3', 'widgets', 'widgets', 'build-box', 'auth', '2026-10-21T09:00:00Z');
`); err != nil {
		t.Fatal(err)
	}

	if err := goose.DownTo(db, "migrations", 224); err != nil {
		t.Fatalf("down to 224: %v", err)
	}
	var columns int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('remote_worker_links') WHERE name = 'source'`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if columns != 0 {
		t.Fatal("source column survived the down migration")
	}

	if err := goose.UpTo(db, "migrations", 225); err != nil {
		t.Fatalf("up to 225: %v", err)
	}
	var source string
	if err := db.QueryRow(`SELECT source FROM remote_worker_links WHERE host_id = 'host-build' AND remote_session_id = 'widgets-3'`).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if source != "spawn" {
		t.Fatalf("source = %q, want spawn for a pre-existing row", source)
	}
}
