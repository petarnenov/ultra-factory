-- ADR 0010 section 7 step 4: termination also writes the root escalation.
-- The key carries the termination time, so a replay writes nothing twice.
-- +goose Up
-- +goose StatementBegin
DROP TRIGGER IF EXISTS coordinator_termination_releases_owners;
CREATE TRIGGER coordinator_termination_releases_owners
AFTER UPDATE OF is_terminated ON sessions
WHEN OLD.kind = 'coordinator' AND OLD.is_terminated = 0 AND NEW.is_terminated = 1
BEGIN
    INSERT OR IGNORE INTO coordinator_summaries (
        id, coordinator_session_id, project_id, submission_key, scope, state, result,
        escalation_reason, escalation_note, created_at, available_at
    )
    SELECT
        'csum_' || lower(hex(randomblob(16))), NEW.id, NEW.project_id,
        'coordinator_terminated:' || NEW.updated_at, NEW.coordinator_scope, 'blocked',
        'The coordinator terminated; its workers were returned to the root.',
        'coordinator_terminated', '', NEW.updated_at, NEW.updated_at
    WHERE NEW.project_id IS NOT NULL;

    INSERT OR IGNORE INTO coordinator_summary_workers (summary_id, worker_session_id, position)
    SELECT cs.id, w.id,
           (SELECT count(*) FROM sessions p WHERE p.owner_session_id = NEW.id AND p.id < w.id)
    FROM sessions w
    JOIN coordinator_summaries cs
      ON cs.coordinator_session_id = NEW.id
     AND cs.submission_key = 'coordinator_terminated:' || NEW.updated_at
    WHERE w.owner_session_id = NEW.id;

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
-- +goose StatementBegin
DROP TRIGGER IF EXISTS coordinator_termination_releases_owners;
CREATE TRIGGER coordinator_termination_releases_owners
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
