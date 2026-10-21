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
  AND NOT EXISTS (SELECT 1 FROM report_pull_acks a WHERE a.report_id = reports.id)
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
  AND NOT EXISTS (SELECT 1 FROM report_pull_acks a WHERE a.report_id = r.id)
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
  AND NOT EXISTS (SELECT 1 FROM report_pull_acks a WHERE a.report_id = reports.id)
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
  AND NOT EXISTS (SELECT 1 FROM report_pull_acks a WHERE a.report_id = reports.id)
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

-- name: TouchReportPuller :exec
INSERT INTO report_pullers (session_id, puller_id, expired_count, last_pulled_at)
VALUES (sqlc.arg(session_id), sqlc.arg(puller_id), 0, sqlc.arg(pulled_at))
ON CONFLICT (session_id, puller_id) DO UPDATE SET last_pulled_at = excluded.last_pulled_at;

-- name: ReduceReportPullerExpired :exec
UPDATE report_pullers
SET expired_count = MAX(0, expired_count - sqlc.arg(seen))
WHERE session_id = sqlc.arg(session_id) AND puller_id = sqlc.arg(puller_id);

-- name: GetReportPullerExpired :one
SELECT expired_count FROM report_pullers
WHERE session_id = sqlc.arg(session_id) AND puller_id = sqlc.arg(puller_id);

-- name: ListReportPullerAckedIDs :many
SELECT a.report_id FROM report_pull_acks a
JOIN reports r ON r.id = a.report_id
WHERE r.session_id = sqlc.arg(session_id) AND a.puller_id = sqlc.arg(puller_id);

-- name: AcknowledgeReportForPuller :execrows
INSERT INTO report_pull_acks (report_id, puller_id, acknowledged_at)
SELECT id, sqlc.arg(puller_id), sqlc.arg(acknowledged_at)
FROM reports
WHERE id = sqlc.arg(id) AND session_id = sqlc.arg(session_id)
ON CONFLICT (report_id, puller_id) DO NOTHING;

-- name: AddExpiredToReportPullers :exec
UPDATE report_pullers
SET expired_count = expired_count + (
    SELECT COUNT(*) FROM reports r
    WHERE r.session_id = sqlc.arg(session_id)
      AND r.delivery_state = 'pending'
      AND (r.created_at < sqlc.arg(cutoff)
           OR r.id NOT IN (SELECT k.id FROM reports k WHERE k.session_id = sqlc.arg(session_id)
                           ORDER BY k.created_at DESC, k.id DESC LIMIT sqlc.arg(keep)))
      AND NOT EXISTS (SELECT 1 FROM report_pull_acks a
                      WHERE a.report_id = r.id AND a.puller_id = report_pullers.puller_id)
)
WHERE report_pullers.session_id = sqlc.arg(session_id);

-- name: ExpirePendingSessionReports :execrows
UPDATE reports
SET delivery_state = 'acknowledged', claim_token = 'expired',
    claimed_at = sqlc.arg(expired_at), acknowledged_at = sqlc.arg(expired_at),
    last_error = 'expired before every puller pulled it'
WHERE reports.session_id = sqlc.arg(session_id) AND reports.delivery_state = 'pending'
  AND (reports.created_at < sqlc.arg(cutoff)
       OR reports.id NOT IN (SELECT k.id FROM reports k WHERE k.session_id = sqlc.arg(session_id)
                             ORDER BY k.created_at DESC, k.id DESC LIMIT sqlc.arg(keep)));
