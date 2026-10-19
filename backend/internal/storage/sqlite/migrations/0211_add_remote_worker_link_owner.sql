-- The local coordinator owning a linked remote worker. NULL means the link's
-- project root; the remote parent is never stored.
-- +goose Up
ALTER TABLE remote_worker_links ADD COLUMN owner_session_id TEXT REFERENCES sessions (id);

-- +goose Down
ALTER TABLE remote_worker_links DROP COLUMN owner_session_id;
