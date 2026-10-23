-- +goose Up
-- +goose StatementBegin
ALTER TABLE reports ADD COLUMN terminal_outcome TEXT NOT NULL DEFAULT ''
    CHECK (terminal_outcome IN ('', 'success', 'failure'));

CREATE TABLE report_terminal_completions (
    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    report_id TEXT NOT NULL UNIQUE REFERENCES reports(id) ON DELETE CASCADE,
    outcome TEXT NOT NULL CHECK (outcome IN ('success', 'failure')),
    created_at TIMESTAMP NOT NULL
);

ALTER TABLE remote_worker_reports ADD COLUMN terminal_outcome TEXT NOT NULL DEFAULT ''
    CHECK (terminal_outcome IN ('', 'success', 'failure'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE remote_worker_reports DROP COLUMN terminal_outcome;
DROP TABLE IF EXISTS report_terminal_completions;
ALTER TABLE reports DROP COLUMN terminal_outcome;
-- +goose StatementEnd
