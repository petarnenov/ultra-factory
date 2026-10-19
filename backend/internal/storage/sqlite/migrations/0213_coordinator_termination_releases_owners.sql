-- Coordinator termination releases its workers and remote links atomically.
-- CO-2.2.2 adds the root recovery summary once summary tables exist.
-- +goose Up
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS coordinator_termination_releases_owners
AFTER UPDATE OF is_terminated ON sessions
WHEN OLD.kind = 'coordinator' AND OLD.is_terminated = 0 AND NEW.is_terminated = 1
BEGIN
    INSERT INTO session_owner_events (
        session_id, previous_owner_session_id, new_owner_session_id, reason, created_at
    )
    SELECT id, owner_session_id, NULL, 'coordinator_terminated', NEW.updated_at
    FROM sessions
    WHERE owner_session_id = NEW.id;

    UPDATE sessions SET owner_session_id = NULL WHERE owner_session_id = NEW.id;

    INSERT INTO remote_worker_owner_events (
        host_id, remote_session_id, previous_owner_session_id, new_owner_session_id, reason, created_at
    )
    SELECT host_id, remote_session_id, owner_session_id, NULL, 'coordinator_terminated', NEW.updated_at
    FROM remote_worker_links
    WHERE owner_session_id = NEW.id;

    UPDATE remote_worker_links SET owner_session_id = NULL WHERE owner_session_id = NEW.id;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS coordinator_termination_releases_owners;
