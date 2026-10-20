-- session_ci_gate_events audits each draft/ready flip the daemon makes from
-- an AO review verdict. One row per (pr, head, kind) is the idempotency key.
-- +goose Up
-- +goose StatementBegin
CREATE TABLE session_ci_gate_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    pr_url TEXT NOT NULL,
    head_sha TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('ci_gate_ready', 'ci_gate_draft')),
    reason TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_session_ci_gate_events_session
    ON session_ci_gate_events (session_id, id);
-- +goose StatementEnd

-- +goose Down
DROP TABLE session_ci_gate_events;
