package sqlite

import (
	"testing"

	"github.com/pressly/goose/v3"
)

// TestReportPullAcksMigrationDownRemovesPullerState checks 0226 both ways: the
// tables exist after up, and down removes them while leaving reports intact.
func TestReportPullAcksMigrationDownRemovesPullerState(t *testing.T) {
	db := openMigratedTestDB(t)
	for _, table := range []string{"report_pull_acks", "report_pullers"} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n); err != nil || n != 1 {
			t.Fatalf("table %s after up: n=%d err=%v", table, n, err)
		}
	}
	if err := goose.DownTo(db, "migrations", 225); err != nil {
		t.Fatalf("down migration 226: %v", err)
	}
	for _, table := range []string{"report_pull_acks", "report_pullers"} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("table %s after down: n=%d err=%v", table, n, err)
		}
	}
	if err := goose.UpTo(db, "migrations", 226); err != nil {
		t.Fatalf("up again: %v", err)
	}
}
