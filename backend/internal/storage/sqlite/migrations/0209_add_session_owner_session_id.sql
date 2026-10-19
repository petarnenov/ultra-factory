-- A worker's owning coordinator. NULL means root-owned. Deliberately not
-- ON DELETE SET NULL: termination is the path that releases workers.
-- +goose Up
ALTER TABLE sessions ADD COLUMN owner_session_id TEXT REFERENCES sessions (id);

-- +goose Down
ALTER TABLE sessions DROP COLUMN owner_session_id;
