-- The CI gate resolved at spawn: whether this session opens draft-first.
-- Set once from the project's ciGate config and never updated afterwards.
-- session_pr_ready_events audits each manual draft/ready flip.
-- +goose Up
ALTER TABLE sessions ADD COLUMN ci_gate_active INTEGER NOT NULL DEFAULT 0;
-- +goose StatementBegin
CREATE TABLE session_pr_ready_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    actor_session_id TEXT NOT NULL DEFAULT '',
    pr_url TEXT NOT NULL,
    head_sha TEXT NOT NULL,
    ready INTEGER NOT NULL CHECK (ready IN (0, 1)),
    created_at TIMESTAMP NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_session_pr_ready_events_session
    ON session_pr_ready_events (session_id, id);
-- +goose StatementEnd

-- +goose Down
DROP TABLE session_pr_ready_events;
ALTER TABLE sessions DROP COLUMN ci_gate_active;
