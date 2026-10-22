-- name: ClaimPRMergeAnnouncement :execrows
INSERT OR IGNORE INTO pr_merge_announcements (merge_commit_sha, pr_url, session_id, announced_at)
VALUES (?, ?, ?, ?);

-- name: ReleasePRMergeAnnouncement :exec
DELETE FROM pr_merge_announcements WHERE merge_commit_sha = ?;
