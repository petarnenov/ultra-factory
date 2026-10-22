-- +goose Up
ALTER TABLE coordinator_summary_remote_workers
    ADD COLUMN host_label TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE coordinator_summary_remote_workers
    DROP COLUMN host_label;
