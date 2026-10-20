-- Append-only ownership audit. Owner values are plain IDs so a row still
-- names an owner that was later removed; each row cascades with its subject.
-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS session_owner_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    previous_owner_session_id TEXT,
    new_owner_session_id TEXT,
    reason TEXT NOT NULL CHECK (reason IN (
        'spawn_parent', 'explicit_assignment', 'explicit_reassignment',
        'coordinator_terminated', 'owner_not_live_reconciled'
    )),
    created_at TIMESTAMP NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_session_owner_events_session
    ON session_owner_events (session_id, id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS remote_worker_owner_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    host_id TEXT NOT NULL,
    remote_session_id TEXT NOT NULL,
    previous_owner_session_id TEXT,
    new_owner_session_id TEXT,
    reason TEXT NOT NULL CHECK (reason IN (
        'spawn_parent', 'explicit_assignment', 'explicit_reassignment',
        'coordinator_terminated', 'owner_not_live_reconciled'
    )),
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY (host_id, remote_session_id)
        REFERENCES remote_worker_links (host_id, remote_session_id) ON DELETE CASCADE
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_remote_worker_owner_events_link
    ON remote_worker_owner_events (host_id, remote_session_id, id);
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS remote_worker_owner_events;
DROP TABLE IF EXISTS session_owner_events;
