-- +goose Up
-- +goose StatementBegin
-- Completed-task reports need the configuration a task ran with over time,
-- not only its final value. Triggers record every committed change to the
-- session's harness, model, or effort and to a Chat conversation's per-turn
-- model and reasoning-effort overrides. Rows written before this migration do
-- not exist; reports show that history as unavailable rather than guessing.
CREATE TABLE session_config_history (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id  TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    source      TEXT NOT NULL CHECK (source IN ('session', 'conversation')),
    harness     TEXT NOT NULL DEFAULT '',
    model       TEXT NOT NULL DEFAULT '',
    effort      TEXT NOT NULL DEFAULT '',
    recorded_at TIMESTAMP NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_session_config_history_session ON session_config_history (session_id, id);

CREATE TRIGGER session_config_history_insert
AFTER INSERT ON sessions
BEGIN
    INSERT INTO session_config_history (session_id, source, harness, model, effort)
    VALUES (NEW.id, 'session', NEW.harness, NEW.model, NEW.effort);
END;

CREATE TRIGGER session_config_history_update
AFTER UPDATE OF harness, model, effort ON sessions
WHEN OLD.harness IS NOT NEW.harness OR OLD.model IS NOT NEW.model OR OLD.effort IS NOT NEW.effort
BEGIN
    INSERT INTO session_config_history (session_id, source, harness, model, effort)
    VALUES (NEW.id, 'session', NEW.harness, NEW.model, NEW.effort);
END;

-- A NULL conversation override means "use the session's configuration"; it is
-- stored as an empty string so the report falls back to the session row.
CREATE TRIGGER conversation_config_history_update
AFTER UPDATE OF model, reasoning_effort ON conversations
WHEN NEW.current_session_id IS NOT NULL
 AND (OLD.model IS NOT NEW.model OR OLD.reasoning_effort IS NOT NEW.reasoning_effort)
BEGIN
    INSERT INTO session_config_history (session_id, source, harness, model, effort)
    SELECT s.id, 'conversation', s.harness, COALESCE(NEW.model, ''), COALESCE(NEW.reasoning_effort, '')
    FROM sessions s WHERE s.id = NEW.current_session_id;
END;

-- session_stage_transitions is an append-only observation log of the derived
-- Kanban column. The stage-history observer appends a row when the column it
-- derives differs from the session's latest row. It is history for reports
-- only: current status is still derived from durable facts on every read.
CREATE TABLE session_stage_transitions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    stage      TEXT NOT NULL
        CHECK (stage IN ('building', 'validating', 'needs_review', 'ready', 'archive')),
    entered_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_session_stage_transitions_session ON session_stage_transitions (session_id, id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS session_stage_transitions;
DROP TRIGGER IF EXISTS conversation_config_history_update;
DROP TRIGGER IF EXISTS session_config_history_update;
DROP TRIGGER IF EXISTS session_config_history_insert;
DROP TABLE IF EXISTS session_config_history;
-- +goose StatementEnd
