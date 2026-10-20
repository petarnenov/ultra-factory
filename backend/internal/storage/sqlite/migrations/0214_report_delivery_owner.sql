-- The recipient a claimed report batch was accepted for. It is frozen at
-- claim time for diagnostics; routing always re-resolves the current owner.
-- +goose Up
ALTER TABLE reports ADD COLUMN delivery_owner_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE reports DROP COLUMN delivery_owner_id;
