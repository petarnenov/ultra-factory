-- +goose Up
-- +goose StatementBegin
CREATE TABLE remote_worker_reports (
    host_id TEXT NOT NULL,
    remote_session_id TEXT NOT NULL,
    report_id TEXT NOT NULL CHECK (length(trim(report_id)) > 0),
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source_project_id TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT '' CHECK (state IN ('', 'checkpoint', 'needs_input', 'stuck', 'done')),
    note TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 1000),
    message TEXT NOT NULL DEFAULT '' CHECK (length(message) <= 1000),
    agent TEXT NOT NULL DEFAULT '',
    session_name TEXT NOT NULL DEFAULT '',
    host_label TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    repeat_count INTEGER NOT NULL DEFAULT 1 CHECK (repeat_count >= 1),
    PRIMARY KEY (host_id, remote_session_id, report_id)
);

CREATE TABLE remote_worker_report_outputs (
    host_id TEXT NOT NULL,
    remote_session_id TEXT NOT NULL,
    report_id TEXT NOT NULL,
    position INTEGER NOT NULL CHECK (position >= 0),
    kind TEXT NOT NULL CHECK (kind IN ('artifact', 'pr_created', 'pr_reviewed')),
    reference TEXT NOT NULL CHECK (length(trim(reference)) > 0),
    label TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (host_id, remote_session_id, report_id, position),
    FOREIGN KEY (host_id, remote_session_id, report_id)
        REFERENCES remote_worker_reports(host_id, remote_session_id, report_id) ON DELETE CASCADE
);

CREATE INDEX idx_remote_worker_reports_project
    ON remote_worker_reports(project_id, created_at, report_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_remote_worker_reports_project;
DROP TABLE IF EXISTS remote_worker_report_outputs;
DROP TABLE IF EXISTS remote_worker_reports;
-- +goose StatementEnd
