-- A coordinator's own worker limit: NULL means the project's
-- maxWorkersPerCoordinator. Value rules beyond >= 1 live in the service.
-- Limit changes on a live coordinator append coordinator_limit_events.
-- +goose Up
ALTER TABLE sessions ADD COLUMN coordinator_worker_limit INTEGER
    CHECK (coordinator_worker_limit IS NULL OR coordinator_worker_limit >= 1);
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS coordinator_limit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    previous_limit INTEGER,
    new_limit INTEGER NOT NULL CHECK (new_limit >= 1),
    actor_session_id TEXT NOT NULL,
    origin TEXT NOT NULL CHECK (length(origin) BETWEEN 1 AND 64),
    created_at TIMESTAMP NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_coordinator_limit_events_session
    ON coordinator_limit_events (session_id, id);
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS coordinator_limit_events;
ALTER TABLE sessions DROP COLUMN coordinator_worker_limit;
