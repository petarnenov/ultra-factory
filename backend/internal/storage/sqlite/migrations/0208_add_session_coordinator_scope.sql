-- A coordinator's bounded area of work: required for coordinators, empty for
-- other kinds, immutable after insert. Value rules live in 0212's triggers.
-- +goose Up
ALTER TABLE sessions ADD COLUMN coordinator_scope TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sessions DROP COLUMN coordinator_scope;
