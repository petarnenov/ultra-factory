-- Agent profile of a session: the project profile ID that picked its harness,
-- model and effort (NULL when none did). A coordinator also stores the worker
-- agent selection it spawns under (JSON; NULL means the project default).
-- Live selection changes append coordinator_agent_events.
-- +goose Up
ALTER TABLE sessions ADD COLUMN agent_profile_id TEXT
    CHECK (agent_profile_id IS NULL OR length(agent_profile_id) BETWEEN 1 AND 128);
ALTER TABLE sessions ADD COLUMN coordinator_worker_agent TEXT
    CHECK (coordinator_worker_agent IS NULL OR json_valid(coordinator_worker_agent));
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS coordinator_agent_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    previous_selection TEXT CHECK (previous_selection IS NULL OR json_valid(previous_selection)),
    new_selection TEXT NOT NULL CHECK (json_valid(new_selection)),
    actor_session_id TEXT NOT NULL,
    origin TEXT NOT NULL CHECK (length(origin) BETWEEN 1 AND 64),
    created_at TIMESTAMP NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_coordinator_agent_events_session
    ON coordinator_agent_events (session_id, id);
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS coordinator_agent_events;
ALTER TABLE sessions DROP COLUMN coordinator_worker_agent;
ALTER TABLE sessions DROP COLUMN agent_profile_id;
