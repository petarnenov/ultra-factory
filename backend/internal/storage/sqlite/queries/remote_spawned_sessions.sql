-- name: InsertRemoteSpawnedSession :exec
INSERT INTO remote_spawned_sessions (session_id, created_at) VALUES (?, ?)
ON CONFLICT(session_id) DO NOTHING;

-- name: ListActiveRemoteSpawnedSessions :many
SELECT s.id FROM remote_spawned_sessions r
JOIN sessions s ON s.id = r.session_id
WHERE s.is_terminated = 0
ORDER BY r.created_at, s.id;
