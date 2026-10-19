-- ADR 0010 ownership invariants I2-I4 as the durable write boundary.
-- The service validates first; these triggers backstop every other writer.
-- +goose Up
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS sessions_ownership_insert
BEFORE INSERT ON sessions
BEGIN
    SELECT RAISE(ABORT, 'coordinator scope is required for kind=coordinator')
    WHERE NEW.kind = 'coordinator' AND length(trim(NEW.coordinator_scope)) = 0;
    SELECT RAISE(ABORT, 'coordinator scope is only valid for kind=coordinator')
    WHERE NEW.kind <> 'coordinator' AND NEW.coordinator_scope <> '';
    SELECT RAISE(ABORT, 'only workers may have an owner')
    WHERE NEW.owner_session_id IS NOT NULL AND NEW.kind <> 'worker';
    SELECT RAISE(ABORT, 'owner must be a live coordinator in the same project')
    WHERE NEW.owner_session_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM sessions o
        WHERE o.id = NEW.owner_session_id
          AND o.kind = 'coordinator'
          AND o.is_terminated = 0
          AND o.project_id = NEW.project_id
    );
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS sessions_ownership_update
BEFORE UPDATE OF owner_session_id, kind, coordinator_scope ON sessions
BEGIN
    SELECT RAISE(ABORT, 'coordinator scope is immutable')
    WHERE OLD.coordinator_scope <> NEW.coordinator_scope
      AND NOT (OLD.is_task_preparation = 1 AND OLD.kind = 'worker');
    SELECT RAISE(ABORT, 'coordinator scope is required for kind=coordinator')
    WHERE NEW.kind = 'coordinator' AND length(trim(NEW.coordinator_scope)) = 0;
    SELECT RAISE(ABORT, 'coordinator scope is only valid for kind=coordinator')
    WHERE NEW.kind <> 'coordinator' AND NEW.coordinator_scope <> '';
    SELECT RAISE(ABORT, 'only workers may have an owner')
    WHERE NEW.owner_session_id IS NOT NULL AND NEW.kind <> 'worker';
    SELECT RAISE(ABORT, 'owner must be a live coordinator in the same project')
    WHERE NEW.owner_session_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM sessions o
        WHERE o.id = NEW.owner_session_id
          AND o.kind = 'coordinator'
          AND o.is_terminated = 0
          AND o.project_id = NEW.project_id
    );
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS sessions_owner_event_insert
AFTER INSERT ON sessions
WHEN NEW.owner_session_id IS NOT NULL
BEGIN
    INSERT INTO session_owner_events (
        session_id, previous_owner_session_id, new_owner_session_id, reason, created_at
    ) VALUES (NEW.id, NULL, NEW.owner_session_id, 'spawn_parent', NEW.created_at);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS remote_worker_links_ownership_insert
BEFORE INSERT ON remote_worker_links
BEGIN
    SELECT RAISE(ABORT, 'remote worker owner must be a live coordinator in the link project')
    WHERE NEW.owner_session_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM sessions o
        WHERE o.id = NEW.owner_session_id
          AND o.kind = 'coordinator'
          AND o.is_terminated = 0
          AND o.project_id = NEW.project_id
    );
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS remote_worker_links_ownership_update
BEFORE UPDATE OF project_id, owner_session_id ON remote_worker_links
BEGIN
    SELECT RAISE(ABORT, 'remote worker link project is immutable')
    WHERE OLD.project_id <> NEW.project_id;
    SELECT RAISE(ABORT, 'remote worker owner must be a live coordinator in the link project')
    WHERE NEW.owner_session_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM sessions o
        WHERE o.id = NEW.owner_session_id
          AND o.kind = 'coordinator'
          AND o.is_terminated = 0
          AND o.project_id = NEW.project_id
    );
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS remote_worker_owner_event_insert
AFTER INSERT ON remote_worker_links
WHEN NEW.owner_session_id IS NOT NULL
BEGIN
    INSERT INTO remote_worker_owner_events (
        host_id, remote_session_id, previous_owner_session_id, new_owner_session_id, reason, created_at
    ) VALUES (NEW.host_id, NEW.remote_session_id, NULL, NEW.owner_session_id, 'spawn_parent', NEW.created_at);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_sessions_owner
    ON sessions (owner_session_id, is_terminated);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_remote_worker_links_owner
    ON remote_worker_links (owner_session_id);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_remote_worker_links_project
    ON remote_worker_links (project_id);
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS remote_worker_owner_event_insert;
DROP TRIGGER IF EXISTS remote_worker_links_ownership_update;
DROP TRIGGER IF EXISTS remote_worker_links_ownership_insert;
DROP TRIGGER IF EXISTS sessions_owner_event_insert;
DROP TRIGGER IF EXISTS sessions_ownership_update;
DROP TRIGGER IF EXISTS sessions_ownership_insert;
DROP INDEX IF EXISTS idx_remote_worker_links_project;
DROP INDEX IF EXISTS idx_remote_worker_links_owner;
DROP INDEX IF EXISTS idx_sessions_owner;
