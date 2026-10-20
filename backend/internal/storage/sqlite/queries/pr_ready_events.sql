-- name: InsertSessionPRReadyEvent :exec
INSERT INTO session_pr_ready_events (
    session_id, actor_session_id, pr_url, head_sha, ready, created_at
) VALUES (?, ?, ?, ?, ?, ?);

-- name: ListSessionPRReadyEvents :many
SELECT id, session_id, actor_session_id, pr_url, head_sha, ready, created_at
FROM session_pr_ready_events
WHERE session_id = ?
ORDER BY id;
