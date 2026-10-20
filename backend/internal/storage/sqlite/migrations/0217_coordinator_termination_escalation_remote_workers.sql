-- ADR 0010 section 7 step 4: the termination escalation also lists released
-- remote links, and its key is random under the reserved daemon: prefix.
-- +goose Up
-- +goose StatementBegin
CREATE TABLE coordinator_summary_remote_workers (
    summary_id TEXT NOT NULL REFERENCES coordinator_summaries(id) ON DELETE CASCADE,
    host_id TEXT NOT NULL,
    remote_session_id TEXT NOT NULL,
    position INTEGER NOT NULL CHECK (position >= 0),
    PRIMARY KEY (summary_id, host_id, remote_session_id)
);

DROP TRIGGER IF EXISTS coordinator_termination_releases_owners;
CREATE TRIGGER coordinator_termination_releases_owners
AFTER UPDATE OF is_terminated ON sessions
WHEN OLD.kind = 'coordinator' AND OLD.is_terminated = 0 AND NEW.is_terminated = 1
BEGIN
    INSERT INTO coordinator_summaries (
        id, coordinator_session_id, project_id, submission_key, scope, state, result,
        escalation_reason, escalation_note, created_at, available_at
    )
    SELECT
        'csum_' || lower(hex(randomblob(16))), NEW.id, NEW.project_id,
        'daemon:coordinator_terminated:' || lower(hex(randomblob(8))),
        NEW.coordinator_scope, 'blocked',
        'The coordinator terminated; its workers were returned to the root.',
        'coordinator_terminated', '', NEW.updated_at, NEW.updated_at
    WHERE NEW.project_id IS NOT NULL;

    INSERT INTO coordinator_summary_workers (summary_id, worker_session_id, position)
    SELECT cs.id, w.id,
           (SELECT count(*) FROM sessions p WHERE p.owner_session_id = NEW.id AND p.id < w.id)
    FROM sessions w
    JOIN coordinator_summaries cs
      ON cs.coordinator_session_id = NEW.id
     AND cs.id = (
            SELECT p.id FROM coordinator_summaries p
            WHERE p.coordinator_session_id = NEW.id
              AND p.escalation_reason = 'coordinator_terminated'
            ORDER BY p.rowid DESC LIMIT 1)
    WHERE w.owner_session_id = NEW.id;

    INSERT INTO coordinator_summary_remote_workers (summary_id, host_id, remote_session_id, position)
    SELECT cs.id, l.host_id, l.remote_session_id,
           (SELECT count(*) FROM remote_worker_links p
            WHERE p.owner_session_id = NEW.id
              AND (p.host_id < l.host_id
                   OR (p.host_id = l.host_id AND p.remote_session_id < l.remote_session_id)))
    FROM remote_worker_links l
    JOIN coordinator_summaries cs
      ON cs.coordinator_session_id = NEW.id
     AND cs.id = (
            SELECT p.id FROM coordinator_summaries p
            WHERE p.coordinator_session_id = NEW.id
              AND p.escalation_reason = 'coordinator_terminated'
            ORDER BY p.rowid DESC LIMIT 1)
    WHERE l.owner_session_id = NEW.id;

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
DROP TABLE IF EXISTS coordinator_summary_remote_workers;
-- +goose StatementEnd
