-- name: SetSessionOwner :execrows
-- Narrow owner write: a stale session snapshot cannot replay other facts.
-- The caller reads the previous owner and appends audit in one transaction.
UPDATE sessions
SET owner_session_id = sqlc.narg(owner_session_id),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND kind = 'worker';

-- name: SetSessionOwnerUnlessReportClaimed :execrows
-- Explicit reassignment refuses while a report is claimed; the condition is
-- part of the owner write so a claim cannot land between check and update.
UPDATE sessions
SET owner_session_id = sqlc.narg(owner_session_id),
    updated_at = sqlc.arg(updated_at)
WHERE sessions.id = sqlc.arg(id) AND sessions.kind = 'worker'
  AND NOT EXISTS (
      SELECT 1 FROM reports r
      WHERE r.session_id = sessions.id AND r.delivery_state = 'claimed'
  );

-- name: InsertSessionOwnerEvent :exec
INSERT INTO session_owner_events (
    session_id, previous_owner_session_id, new_owner_session_id, reason, created_at
) VALUES (?, ?, ?, ?, ?);

-- name: ListSessionOwnerEvents :many
SELECT id, session_id, previous_owner_session_id, new_owner_session_id, reason, created_at
FROM session_owner_events
WHERE session_id = ?
ORDER BY id;

-- name: SetRemoteWorkerOwner :execrows
-- Only the explicit ownership path touches this column; re-registration
-- cannot move a link between projects or replace its owner.
UPDATE remote_worker_links
SET owner_session_id = sqlc.narg(owner_session_id)
WHERE host_id = sqlc.arg(host_id) AND remote_session_id = sqlc.arg(remote_session_id);

-- name: InsertRemoteWorkerOwnerEvent :exec
INSERT INTO remote_worker_owner_events (
    host_id, remote_session_id, previous_owner_session_id, new_owner_session_id, reason, created_at
) VALUES (?, ?, ?, ?, ?, ?);

-- name: ListRemoteWorkerOwnerEvents :many
SELECT id, host_id, remote_session_id, previous_owner_session_id, new_owner_session_id, reason, created_at
FROM remote_worker_owner_events
WHERE host_id = ? AND remote_session_id = ?
ORDER BY id;

-- name: SetCoordinatorWorkerLimit :execrows
-- Narrow write: only a live coordinator's own limit changes.
UPDATE sessions
SET coordinator_worker_limit = sqlc.narg(coordinator_worker_limit),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND kind = 'coordinator' AND is_terminated = 0;

-- name: InsertCoordinatorLimitEvent :exec
INSERT INTO coordinator_limit_events (
    session_id, previous_limit, new_limit, actor_session_id, origin, created_at
) VALUES (?, ?, ?, ?, ?, ?);

-- name: ListCoordinatorLimitEvents :many
SELECT id, session_id, previous_limit, new_limit, actor_session_id, origin, created_at
FROM coordinator_limit_events
WHERE session_id = ?
ORDER BY id;

-- name: SetCoordinatorWorkerAgent :execrows
-- Narrow write: only a live coordinator's worker agent selection changes.
UPDATE sessions
SET coordinator_worker_agent = sqlc.narg(coordinator_worker_agent),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND kind = 'coordinator' AND is_terminated = 0;

-- name: InsertCoordinatorAgentEvent :exec
INSERT INTO coordinator_agent_events (
    session_id, previous_selection, new_selection, actor_session_id, origin, created_at
) VALUES (?, ?, ?, ?, ?, ?);

-- name: ListCoordinatorAgentEvents :many
SELECT id, session_id, previous_selection, new_selection, actor_session_id, origin, created_at
FROM coordinator_agent_events
WHERE session_id = ?
ORDER BY id;
