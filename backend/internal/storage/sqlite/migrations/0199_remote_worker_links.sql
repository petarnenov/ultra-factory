-- +goose Up
-- +goose StatementBegin
-- A worker spawned on a saved remote host with `ao spawn --host` reports into
-- that host's daemon. This daemon pulls those reports and delivers them to the
-- linked project's active orchestrator. Remote IDs are the remote daemon's own,
-- so they carry no foreign key here.
CREATE TABLE remote_worker_links (
    host_id TEXT NOT NULL CHECK (length(trim(host_id)) > 0),
    remote_session_id TEXT NOT NULL CHECK (length(trim(remote_session_id)) > 0),
    remote_project_id TEXT NOT NULL CHECK (length(trim(remote_project_id)) > 0),
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    host_label TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    PRIMARY KEY (host_id, remote_session_id)
);

-- Remote report IDs already delivered to the local orchestrator. A report the
-- remote daemon still lists (its acknowledgement failed) is acknowledged again
-- but never delivered twice.
CREATE TABLE remote_worker_report_deliveries (
    host_id TEXT NOT NULL,
    remote_session_id TEXT NOT NULL,
    report_id TEXT NOT NULL CHECK (length(trim(report_id)) > 0),
    delivered_at TIMESTAMP NOT NULL,
    PRIMARY KEY (host_id, remote_session_id, report_id),
    FOREIGN KEY (host_id, remote_session_id)
        REFERENCES remote_worker_links(host_id, remote_session_id) ON DELETE CASCADE
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS remote_worker_report_deliveries;
DROP TABLE IF EXISTS remote_worker_links;
-- +goose StatementEnd
