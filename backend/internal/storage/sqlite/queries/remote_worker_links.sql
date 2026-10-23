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

-- name: UpdateRemoteWorkerLinkLiveness :exec
UPDATE remote_worker_links SET last_seen_at = sqlc.arg(last_seen_at), last_state = sqlc.arg(last_state),
    terminated_at = CASE WHEN sqlc.arg(last_state) = 'terminated'
        THEN COALESCE(terminated_at, sqlc.arg(last_seen_at)) ELSE NULL END
WHERE host_id = sqlc.arg(host_id) AND remote_session_id = sqlc.arg(remote_session_id);

-- name: InsertRemoteWorkerLinkRemoval :exec
INSERT INTO remote_worker_link_removals (host_id, remote_session_id, removed_at)
VALUES (?, ?, ?)
ON CONFLICT(host_id, remote_session_id) DO UPDATE SET removed_at = excluded.removed_at;

-- name: ListRemoteWorkerLinkRemovals :many
SELECT * FROM remote_worker_link_removals ORDER BY host_id, remote_session_id;

-- name: DeleteRemoteWorkerLinkRemoval :exec
DELETE FROM remote_worker_link_removals WHERE host_id = ? AND remote_session_id = ?;
