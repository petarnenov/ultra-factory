-- One row per merge commit that AO has already announced to a coordinator.
-- The observer and the orchestrator tick both claim a row before they send,
-- so a merge is announced once whichever path sees it first.
-- +goose Up
CREATE TABLE pr_merge_announcements (
    merge_commit_sha TEXT PRIMARY KEY,
    pr_url           TEXT NOT NULL,
    session_id       TEXT NOT NULL,
    announced_at     TEXT NOT NULL
);

-- +goose Down
DROP TABLE pr_merge_announcements;
