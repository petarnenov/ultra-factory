-- name: GetSessionProgress :one
SELECT * FROM session_progress WHERE session_id = ?;

-- name: ListSessionProgress :many
SELECT * FROM session_progress;

-- name: UpsertSessionProgress :exec
INSERT INTO session_progress (
    session_id, provider_at, tool_at, terminal_at, git_at,
    in_flight_tool, in_flight_tool_at, blocked_at, blocked_reason,
    last_notified_progress_at, stall_count, last_stall_at,
    last_stall_for_seconds
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id) DO UPDATE SET
    provider_at = excluded.provider_at,
    tool_at = excluded.tool_at,
    terminal_at = excluded.terminal_at,
    git_at = excluded.git_at,
    in_flight_tool = excluded.in_flight_tool,
    in_flight_tool_at = excluded.in_flight_tool_at,
    blocked_at = excluded.blocked_at,
    blocked_reason = excluded.blocked_reason,
    last_notified_progress_at = excluded.last_notified_progress_at,
    stall_count = excluded.stall_count,
    last_stall_at = excluded.last_stall_at,
    last_stall_for_seconds = excluded.last_stall_for_seconds;
