-- How a remote worker link was made: "spawn" by `ao spawn --host`, or
-- "discovered" by the daemon's discovery pass. Existing rows were all spawned.
-- +goose Up
ALTER TABLE remote_worker_links ADD COLUMN source TEXT NOT NULL DEFAULT 'spawn';

-- +goose Down
ALTER TABLE remote_worker_links DROP COLUMN source;
