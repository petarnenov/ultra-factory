-- name: InsertCoordinatorSummary :one
INSERT INTO coordinator_summaries (
    id, coordinator_session_id, project_id, submission_key, scope, state, result,
    escalation_reason, escalation_note, created_at, available_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(coordinator_session_id, submission_key) DO NOTHING
RETURNING *;

-- name: GetCoordinatorSummaryByKey :one
SELECT * FROM coordinator_summaries
WHERE coordinator_session_id = ? AND submission_key = ?;

-- name: InsertCoordinatorSummaryWorker :exec
INSERT INTO coordinator_summary_workers (summary_id, worker_session_id, position)
VALUES (?, ?, ?);

-- name: ListCoordinatorSummaryWorkers :many
SELECT worker_session_id FROM coordinator_summary_workers
WHERE summary_id = ?
ORDER BY position;

-- name: ListCoordinatorSummariesBySession :many
SELECT * FROM coordinator_summaries
WHERE coordinator_session_id = ?
ORDER BY created_at DESC, id DESC
LIMIT ?;

-- name: GetLatestAcknowledgedCoordinatorSummary :one
SELECT * FROM coordinator_summaries
WHERE coordinator_session_id = ? AND delivery_state = 'acknowledged'
ORDER BY acknowledged_at DESC, created_at DESC, id DESC
LIMIT 1;

-- name: ListPendingCoordinatorSummarySchedule :many
SELECT * FROM coordinator_summaries
WHERE delivery_state = 'pending'
ORDER BY available_at, created_at, id
LIMIT ?;

-- name: ListPendingCoordinatorSummariesByGroup :many
SELECT * FROM coordinator_summaries
WHERE project_id = sqlc.arg(project_id)
  AND coordinator_session_id = sqlc.arg(coordinator_session_id)
  AND delivery_state = 'pending'
ORDER BY created_at, id;

-- name: AssignPendingCoordinatorSummaryBatch :exec
UPDATE coordinator_summaries
SET delivery_batch_id = sqlc.arg(delivery_batch_id)
WHERE project_id = sqlc.arg(project_id)
  AND coordinator_session_id = sqlc.arg(coordinator_session_id)
  AND delivery_state = 'pending' AND delivery_batch_id = ''
  AND id IN (sqlc.slice(ids));

-- name: ClaimPendingCoordinatorSummaries :many
UPDATE coordinator_summaries
SET delivery_state = 'claimed', claim_token = sqlc.arg(claim_token),
    claimed_at = sqlc.arg(claimed_at), delivery_attempts = delivery_attempts + 1,
    delivery_owner_id = sqlc.arg(root_session_id), last_error = ''
WHERE project_id = sqlc.arg(project_id)
  AND coordinator_session_id = sqlc.arg(coordinator_session_id)
  AND delivery_state = 'pending'
  AND delivery_batch_id = sqlc.arg(delivery_batch_id)
RETURNING *;

-- name: AcknowledgeCoordinatorSummaryBatch :many
UPDATE coordinator_summaries
SET delivery_state = 'acknowledged', acknowledged_at = sqlc.arg(acknowledged_at)
WHERE project_id = sqlc.arg(project_id) AND delivery_state = 'claimed'
  AND claim_token = sqlc.arg(claim_token)
RETURNING *;

-- name: ReleaseCoordinatorSummaryBatch :many
UPDATE coordinator_summaries
SET delivery_state = 'pending', available_at = min(available_at, sqlc.arg(available_at)),
    claim_token = '', claimed_at = NULL, last_error = sqlc.arg(last_error)
WHERE project_id = sqlc.arg(project_id) AND delivery_state = 'claimed'
  AND claim_token = sqlc.arg(claim_token)
RETURNING *;

-- name: DeferCoordinatorSummaryBatch :many
UPDATE coordinator_summaries
SET delivery_state = 'pending', available_at = max(available_at, sqlc.arg(available_at)),
    claim_token = '', claimed_at = NULL, last_error = sqlc.arg(last_error)
WHERE project_id = sqlc.arg(project_id) AND delivery_state = 'claimed'
  AND claim_token = sqlc.arg(claim_token)
RETURNING *;

-- name: RequeueClaimedCoordinatorSummaries :execrows
UPDATE coordinator_summaries
SET delivery_state = 'pending', claim_token = '', claimed_at = NULL,
    last_error = 'delivery claim recovered after daemon restart'
WHERE delivery_state = 'claimed';
