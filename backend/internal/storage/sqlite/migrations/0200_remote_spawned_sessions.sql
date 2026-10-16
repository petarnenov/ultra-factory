-- +goose Up
-- +goose StatementBegin
-- A worker another machine started here with `ao spawn --host` reports to that
-- machine's orchestrator. While such a worker is active this daemon refuses to
-- start an orchestrator of its own, which would otherwise claim those reports.
CREATE TABLE remote_spawned_sessions (
    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS remote_spawned_sessions;
-- +goose StatementEnd
