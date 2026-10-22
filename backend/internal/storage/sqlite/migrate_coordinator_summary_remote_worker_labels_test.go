package sqlite

import (
	"testing"

	"github.com/pressly/goose/v3"
)

func TestCoordinatorSummaryRemoteWorkerLabelsMigrationRoundTrips(t *testing.T) {
	db := openMigratedTestDB(t)
	if _, err := db.Exec(`
INSERT INTO projects (id, path, registered_at) VALUES ('widgets', '/repo/widgets', '2026-10-22T09:00:00Z');
INSERT INTO sessions (id, project_id, num, kind, coordinator_scope, activity_last_at, created_at, updated_at)
VALUES ('coord', 'widgets', 1, 'coordinator', 'migration-test', '2026-10-22T09:00:00Z', '2026-10-22T09:00:00Z', '2026-10-22T09:00:00Z');
INSERT INTO coordinator_summaries (id, coordinator_session_id, project_id, submission_key, scope, state, created_at, available_at)
VALUES ('summary', 'coord', 'widgets', 'key', 'scope', 'working', '2026-10-22T09:00:00Z', '2026-10-22T09:00:00Z');
INSERT INTO coordinator_summary_remote_workers (summary_id, host_id, remote_session_id, position)
VALUES ('summary', 'host-build', 'widgets-3', 0);
`); err != nil {
		t.Fatal(err)
	}
	if err := goose.DownTo(db, "migrations", 228); err != nil {
		t.Fatalf("down to 228: %v", err)
	}
	var columns int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('coordinator_summary_remote_workers') WHERE name = 'host_label'`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if columns != 0 {
		t.Fatalf("host_label column count after down = %d, want 0", columns)
	}
	if err := goose.UpTo(db, "migrations", 229); err != nil {
		t.Fatalf("up to 229: %v", err)
	}
	var label string
	if err := db.QueryRow(`SELECT host_label FROM coordinator_summary_remote_workers WHERE summary_id = 'summary'`).Scan(&label); err != nil {
		t.Fatal(err)
	}
	if label != "" {
		t.Fatalf("migrated host label = %q, want empty for prior summaries", label)
	}
}
