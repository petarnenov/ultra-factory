-- Widen sessions.kind to admit the coordinator kind from ADR 0010.
-- SQLite cannot ALTER a CHECK, so this rewrites the table definition in place.

-- +goose NO TRANSACTION
-- +goose Up
-- +goose StatementBegin
PRAGMA writable_schema = ON;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(
    sql,
    'CHECK (kind IN (''worker'', ''orchestrator''))',
    'CHECK (kind IN (''worker'', ''orchestrator'', ''coordinator''))'
)
WHERE type = 'table' AND name = 'sessions'
  AND sql LIKE '%CHECK (kind IN (%'
  AND sql NOT LIKE '%''coordinator''%';
-- +goose StatementEnd
-- +goose StatementBegin
PRAGMA writable_schema = RESET;
-- +goose StatementEnd

-- +goose Down
-- Abort rather than narrow the CHECK while a coordinator row exists.
-- +goose StatementBegin
CREATE TEMP TABLE IF NOT EXISTS coordinator_kind_down_guard (
    coordinators INTEGER NOT NULL CHECK (coordinators = 0)
);
-- +goose StatementEnd
-- +goose StatementBegin
INSERT INTO coordinator_kind_down_guard (coordinators)
SELECT COUNT(*) FROM sessions WHERE kind = 'coordinator';
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE coordinator_kind_down_guard;
-- +goose StatementEnd
-- +goose StatementBegin
PRAGMA writable_schema = ON;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(
    sql,
    'CHECK (kind IN (''worker'', ''orchestrator'', ''coordinator''))',
    'CHECK (kind IN (''worker'', ''orchestrator''))'
)
WHERE type = 'table' AND name = 'sessions';
-- +goose StatementEnd
-- +goose StatementBegin
PRAGMA writable_schema = RESET;
-- +goose StatementEnd
