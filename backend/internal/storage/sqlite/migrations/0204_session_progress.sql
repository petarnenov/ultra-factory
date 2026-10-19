-- +goose Up
-- +goose StatementBegin
-- Real progress heartbeats per session. These are durable facts only: the last
-- observed timestamp per signal class, the in-flight tool name, and counters.
-- Derived health is never stored; it is computed at read time.
CREATE TABLE session_progress (
    session_id                 TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    provider_at                TIMESTAMP,
    tool_at                    TIMESTAMP,
    terminal_at                TIMESTAMP,
    git_at                     TIMESTAMP,
    in_flight_tool             TEXT NOT NULL DEFAULT '',
    in_flight_tool_at          TIMESTAMP,
    blocked_at                 TIMESTAMP,
    blocked_reason             TEXT NOT NULL DEFAULT '',
    last_notified_progress_at  TIMESTAMP,
    stall_count                INTEGER NOT NULL DEFAULT 0,
    last_stall_at              TIMESTAMP,
    last_stall_for_seconds     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX session_progress_stall_count_idx ON session_progress(stall_count);
-- +goose StatementEnd

-- +goose StatementBegin
-- Only health crossings are broadcast live; heartbeat writes stay quiet.
CREATE TRIGGER session_progress_cdc_health
AFTER UPDATE ON session_progress
WHEN NEW.stall_count <> OLD.stall_count
    OR NEW.last_stall_for_seconds <> OLD.last_stall_for_seconds
    OR NEW.blocked_reason <> OLD.blocked_reason
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    SELECT s.project_id, s.id, 'session_updated',
        json_object('id', s.id, 'healthChanged', json('true')),
        CURRENT_TIMESTAMP
    FROM sessions s
    WHERE s.id = NEW.session_id AND s.project_id IS NOT NULL;
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER session_progress_cdc_health;
DROP INDEX session_progress_stall_count_idx;
DROP TABLE session_progress;
-- +goose StatementEnd
