-- name: InsertSessionCIGateEvent :exec
INSERT INTO session_ci_gate_events (
    session_id, pr_url, head_sha, kind, reason, created_at
) VALUES (?, ?, ?, ?, ?, ?);

-- name: ListSessionCIGateEvents :many
SELECT id, session_id, pr_url, head_sha, kind, reason, created_at
FROM session_ci_gate_events
WHERE session_id = ?
ORDER BY id;
