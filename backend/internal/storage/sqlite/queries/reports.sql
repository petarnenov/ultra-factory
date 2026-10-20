-- name: CreateReport :one
INSERT INTO reports (
    id, session_id, project_id, state, note, message, created_at, available_at,
    settlement_deadline, repeat_count
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: CreateReportOutput :exec
INSERT INTO report_outputs (report_id, position, kind, reference, label)
VALUES (?, ?, ?, ?, ?);

-- name: GetReport :one
SELECT * FROM reports WHERE id = ?;

-- name: ListReportOutputs :many
SELECT * FROM report_outputs WHERE report_id = ? ORDER BY position;

-- name: ListReportsBySession :many
SELECT * FROM reports WHERE session_id = ? ORDER BY created_at, id;

-- name: ListReportedPRURLs :many
SELECT o.reference FROM report_outputs o
JOIN reports r ON r.id = o.report_id
WHERE r.session_id = ? AND o.kind = 'pr_created'
ORDER BY r.created_at, r.id, o.position;

-- name: ListReportsByProject :many
SELECT * FROM reports WHERE project_id = ? ORDER BY created_at, id;

-- name: ListPendingReportSchedule :many
SELECT * FROM reports
WHERE delivery_state = 'pending'
ORDER BY available_at, created_at, id
LIMIT ?;

-- name: ListPendingReportsByRecipientSchedule :many
SELECT r.id, r.session_id, r.project_id, r.state, r.note, r.message, r.created_at,
    r.delivery_state, r.available_at, r.settlement_deadline, r.repeat_count,
    r.claim_token, r.claimed_at, r.delivery_attempts, r.acknowledged_at,
    r.last_error, r.delivery_batch_id, r.delivery_owner_id
FROM reports r
JOIN sessions s ON s.id = r.session_id
WHERE r.project_id = sqlc.arg(project_id) AND r.delivery_state = 'pending'
  AND ((sqlc.arg(fallback_owner_id) IS NULL
        AND COALESCE(s.owner_session_id, sqlc.arg(root_session_id)) = sqlc.arg(recipient_session_id))
       OR s.owner_session_id = sqlc.arg(fallback_owner_id))
ORDER BY r.available_at, r.created_at, r.id
LIMIT sqlc.arg(limit);

-- name: AssignPendingReportsToRecipient :exec
UPDATE reports
SET delivery_batch_id = sqlc.arg(delivery_batch_id)
WHERE reports.project_id = sqlc.arg(project_id) AND reports.delivery_state = 'pending'
  AND reports.delivery_batch_id = ''
  AND reports.session_id IN (
      SELECT id FROM sessions
      WHERE (sqlc.arg(fallback_owner_id) IS NULL
             AND COALESCE(owner_session_id, sqlc.arg(root_session_id)) = sqlc.arg(recipient_session_id))
         OR owner_session_id = sqlc.arg(fallback_owner_id)
  );

-- name: ClaimPendingReportsByRecipient :many
UPDATE reports
SET delivery_state = 'claimed', claim_token = sqlc.arg(claim_token),
    claimed_at = sqlc.arg(claimed_at), delivery_attempts = delivery_attempts + 1,
    delivery_owner_id = sqlc.arg(recipient_session_id), last_error = ''
WHERE reports.project_id = sqlc.arg(project_id) AND reports.delivery_state = 'pending'
  AND reports.delivery_batch_id = sqlc.arg(delivery_batch_id)
  AND reports.session_id IN (
      SELECT id FROM sessions
      WHERE (sqlc.arg(fallback_owner_id) IS NULL
             AND COALESCE(owner_session_id, sqlc.arg(root_session_id)) = sqlc.arg(recipient_session_id))
         OR owner_session_id = sqlc.arg(fallback_owner_id)
  )
RETURNING *;

-- name: AcknowledgeReportBatch :many
UPDATE reports
SET delivery_state = 'acknowledged', acknowledged_at = sqlc.arg(acknowledged_at)
WHERE project_id = sqlc.arg(project_id) AND delivery_state = 'claimed'
  AND claim_token = sqlc.arg(claim_token)
RETURNING *;

-- name: ReleaseReportBatch :many
UPDATE reports
SET delivery_state = 'pending', available_at = min(available_at, sqlc.arg(available_at)),
    claim_token = '', claimed_at = NULL, last_error = sqlc.arg(last_error)
WHERE project_id = sqlc.arg(project_id) AND delivery_state = 'claimed'
  AND claim_token = sqlc.arg(claim_token)
RETURNING *;

-- name: DeferReportBatch :many
UPDATE reports
SET delivery_state = 'pending', available_at = max(available_at, sqlc.arg(available_at)),
    claim_token = '', claimed_at = NULL, last_error = sqlc.arg(last_error)
WHERE project_id = sqlc.arg(project_id) AND delivery_state = 'claimed'
  AND claim_token = sqlc.arg(claim_token)
RETURNING *;

-- name: GetReportInterrupt :one
SELECT last_interrupted_at FROM report_worker_interrupts WHERE session_id = ?;

-- name: PutReportInterrupt :exec
INSERT INTO report_worker_interrupts (session_id, last_interrupted_at)
VALUES (?, ?)
ON CONFLICT(session_id) DO UPDATE SET last_interrupted_at = excluded.last_interrupted_at;

-- name: ListPendingReports :many
SELECT * FROM reports
WHERE delivery_state = 'pending' AND available_at <= ?
ORDER BY created_at, id
LIMIT ?;

-- name: ClaimReport :one
UPDATE reports
SET delivery_state = 'claimed', claim_token = sqlc.arg(claim_token),
    claimed_at = sqlc.arg(claimed_at), delivery_attempts = delivery_attempts + 1,
    last_error = ''
WHERE id = sqlc.arg(id) AND delivery_state = 'pending' AND available_at <= sqlc.arg(claimed_at)
RETURNING *;

-- name: AcknowledgeReport :one
UPDATE reports
SET delivery_state = 'acknowledged', acknowledged_at = sqlc.arg(acknowledged_at)
WHERE id = sqlc.arg(id) AND delivery_state = 'claimed' AND claim_token = sqlc.arg(claim_token)
RETURNING *;

-- name: ReleaseReport :one
UPDATE reports
SET delivery_state = 'pending', available_at = sqlc.arg(available_at),
    claim_token = '', claimed_at = NULL, last_error = sqlc.arg(last_error)
WHERE id = sqlc.arg(id) AND delivery_state = 'claimed' AND claim_token = sqlc.arg(claim_token)
RETURNING *;

-- name: RequeueClaimedReports :execrows
UPDATE reports
SET delivery_state = 'pending', claim_token = '', claimed_at = NULL,
    last_error = 'delivery claim recovered after daemon restart'
WHERE delivery_state = 'claimed';

-- name: AcknowledgePendingSessionReport :execrows
UPDATE reports
SET delivery_state = 'acknowledged', claim_token = sqlc.arg(claim_token),
    claimed_at = sqlc.arg(acknowledged_at), acknowledged_at = sqlc.arg(acknowledged_at),
    delivery_attempts = delivery_attempts + 1, last_error = ''
WHERE id = sqlc.arg(id) AND session_id = sqlc.arg(session_id) AND delivery_state = 'pending';
