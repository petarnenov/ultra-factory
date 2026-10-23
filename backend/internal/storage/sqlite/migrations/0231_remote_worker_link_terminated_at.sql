-- When a link's worker was first read as terminated. The link outlives the
-- worker while its host still watches the worker's PRs for a late outcome.
-- +goose Up
ALTER TABLE remote_worker_links ADD COLUMN terminated_at TIMESTAMP;

-- +goose Down
ALTER TABLE remote_worker_links DROP COLUMN terminated_at;
