-- Operator-initiated coordinator spawns (UI or API on behalf of the live root).
-- One append-only row per spawn, written after the coordinator is created.
-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS coordinator_operator_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    coordinator_session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    project_id TEXT NOT NULL,
    root_session_id TEXT NOT NULL,
    origin TEXT NOT NULL CHECK (length(origin) BETWEEN 1 AND 64),
    scope TEXT NOT NULL,
    worker_limit INTEGER CHECK (worker_limit IS NULL OR worker_limit >= 1),
    host_slots TEXT CHECK (host_slots IS NULL OR json_valid(host_slots)),
    coordinator_agent TEXT CHECK (coordinator_agent IS NULL OR json_valid(coordinator_agent)),
    worker_agent TEXT CHECK (worker_agent IS NULL OR json_valid(worker_agent)),
    created_at TIMESTAMP NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_coordinator_operator_events_session
    ON coordinator_operator_events (coordinator_session_id, id);
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS coordinator_operator_events;
