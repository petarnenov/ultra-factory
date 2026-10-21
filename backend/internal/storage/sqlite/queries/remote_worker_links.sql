-- name: UpsertRemoteWorkerLink :exec
INSERT INTO remote_worker_links (
    host_id, remote_session_id, remote_project_id, project_id, host_label, display_name, owner_session_id, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(host_id, remote_session_id) DO UPDATE SET
    remote_project_id = excluded.remote_project_id,
    host_label = excluded.host_label,
    display_name = excluded.display_name;

-- name: GetRemoteWorkerLink :one
SELECT * FROM remote_worker_links WHERE host_id = ? AND remote_session_id = ?;

-- name: ListRemoteWorkerLinks :many
SELECT * FROM remote_worker_links ORDER BY host_id, created_at, remote_session_id;

-- name: DeleteRemoteWorkerLink :exec
DELETE FROM remote_worker_links WHERE host_id = ? AND remote_session_id = ?;

-- name: ListRemoteWorkerReportDeliveries :many
SELECT report_id FROM remote_worker_report_deliveries
WHERE host_id = ? AND remote_session_id = ?;

-- name: InsertRemoteWorkerReportDelivery :exec
INSERT INTO remote_worker_report_deliveries (host_id, remote_session_id, report_id, delivered_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(host_id, remote_session_id, report_id) DO NOTHING;

-- name: InsertDiscoveredRemoteWorkerLink :execrows
INSERT INTO remote_worker_links (
    host_id, remote_session_id, remote_project_id, project_id, host_label, display_name, owner_session_id, created_at, source
) VALUES (?, ?, ?, ?, ?, ?, NULL, ?, 'discovered')
ON CONFLICT(host_id, remote_session_id) DO NOTHING;
