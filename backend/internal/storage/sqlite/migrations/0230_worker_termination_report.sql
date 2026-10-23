-- A worker's termination files a pending report in the same transaction, so
-- its owner or the root learns the worker ended; remote hosts get it by pull.
-- +goose Up
-- +goose StatementBegin
CREATE TRIGGER worker_termination_files_report
AFTER UPDATE OF is_terminated ON sessions
WHEN OLD.kind = 'worker' AND OLD.is_terminated = 0 AND NEW.is_terminated = 1
    AND NEW.project_id IS NOT NULL AND NEW.project_id <> ''
BEGIN
    INSERT INTO reports (
        id, session_id, project_id, state, note, message, created_at, available_at,
        settlement_deadline, repeat_count
    )
    VALUES (
        'rpt_' || lower(hex(randomblob(16))), NEW.id, NEW.project_id, 'checkpoint',
        'AO: the worker session ended.', '', NEW.updated_at, NEW.updated_at, NULL, 1
    );
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS worker_termination_files_report;
