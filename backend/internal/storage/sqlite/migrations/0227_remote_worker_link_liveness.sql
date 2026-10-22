-- Liveness of a remote worker link, kept apart from report pulling. last_seen_at
-- is the last time the linked host answered a session read for this worker
-- (NULL before the first read, which falls back to created_at). last_state is
-- what that read saw: 'live', 'terminated', or empty before the first read.
-- The removals table remembers links an operator dropped by hand, so discovery
-- does not link a terminated worker again. It carries no foreign key so the
-- memory outlives the link.
-- +goose Up
ALTER TABLE remote_worker_links ADD COLUMN last_seen_at TIMESTAMP;
ALTER TABLE remote_worker_links ADD COLUMN last_state TEXT NOT NULL DEFAULT '';

CREATE TABLE remote_worker_link_removals (
    host_id TEXT NOT NULL CHECK (length(trim(host_id)) > 0),
    remote_session_id TEXT NOT NULL CHECK (length(trim(remote_session_id)) > 0),
    removed_at TIMESTAMP NOT NULL,
    PRIMARY KEY (host_id, remote_session_id)
);

-- +goose Down
DROP TABLE IF EXISTS remote_worker_link_removals;
ALTER TABLE remote_worker_links DROP COLUMN last_state;
ALTER TABLE remote_worker_links DROP COLUMN last_seen_at;
