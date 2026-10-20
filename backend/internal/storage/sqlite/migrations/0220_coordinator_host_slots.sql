-- A coordinator's own host slots: a JSON list of {hostId, slots}, normalized by
-- the service. NULL means no slots. Live slot changes append to
-- coordinator_limit_events, rebuilt so the worker limit may stay NULL and the
-- host slots before and after each change are recorded.
-- +goose Up
ALTER TABLE sessions ADD COLUMN coordinator_host_slots TEXT
    CHECK (coordinator_host_slots IS NULL OR json_valid(coordinator_host_slots));
-- +goose StatementBegin
CREATE TABLE coordinator_limit_events_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    previous_limit INTEGER,
    new_limit INTEGER CHECK (new_limit IS NULL OR new_limit >= 1),
    previous_host_slots TEXT CHECK (previous_host_slots IS NULL OR json_valid(previous_host_slots)),
    new_host_slots TEXT CHECK (new_host_slots IS NULL OR json_valid(new_host_slots)),
    actor_session_id TEXT NOT NULL,
    origin TEXT NOT NULL CHECK (length(origin) BETWEEN 1 AND 64),
    created_at TIMESTAMP NOT NULL
);
-- +goose StatementEnd
INSERT INTO coordinator_limit_events_new (id, session_id, previous_limit, new_limit, actor_session_id, origin, created_at)
SELECT id, session_id, previous_limit, new_limit, actor_session_id, origin, created_at FROM coordinator_limit_events;
DROP TABLE coordinator_limit_events;
ALTER TABLE coordinator_limit_events_new RENAME TO coordinator_limit_events;
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_coordinator_limit_events_session
    ON coordinator_limit_events (session_id, id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TABLE coordinator_limit_events_old (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    previous_limit INTEGER,
    new_limit INTEGER NOT NULL CHECK (new_limit >= 1),
    actor_session_id TEXT NOT NULL,
    origin TEXT NOT NULL CHECK (length(origin) BETWEEN 1 AND 64),
    created_at TIMESTAMP NOT NULL
);
-- +goose StatementEnd
INSERT INTO coordinator_limit_events_old (id, session_id, previous_limit, new_limit, actor_session_id, origin, created_at)
SELECT id, session_id, previous_limit, COALESCE(new_limit, previous_limit, 1), actor_session_id, origin, created_at
FROM coordinator_limit_events;
DROP TABLE coordinator_limit_events;
ALTER TABLE coordinator_limit_events_old RENAME TO coordinator_limit_events;
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_coordinator_limit_events_session
    ON coordinator_limit_events (session_id, id);
-- +goose StatementEnd
ALTER TABLE sessions DROP COLUMN coordinator_host_slots;
