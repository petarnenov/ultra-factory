-- Widen the usage pipeline's own CHECK constraints to allow OpenCode 2, whose
-- usage lives in its SQLite store rather than in a JSONL transcript. SQLite
-- cannot ALTER a CHECK constraint, so this rewrites the two table definitions
-- in place, the same way the harness migrations widen sessions.harness.
--
-- A database whose constraint is missing an earlier value matches neither
-- string on purpose: migrations run before reconcileSchema, so those databases
-- are widened by that repair instead.

-- +goose NO TRANSACTION
-- +goose Up
-- +goose StatementBegin
PRAGMA writable_schema = ON;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(
    sql,
    '''claude-code'', ''codex'', ''kimi''))',
    '''claude-code'', ''codex'', ''kimi'', ''opencode-v2''))'
)
WHERE type = 'table' AND name = 'usage_bindings'
  AND sql LIKE '%CHECK (harness IN (%'
  AND sql NOT LIKE '%''opencode-v2''%';
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(
    sql,
    '''claude_main'', ''claude_subagent'', ''codex_rollout'', ''kimi_wire''))',
    '''claude_main'', ''claude_subagent'', ''codex_rollout'', ''kimi_wire'', ''opencode_v2_session''))'
)
WHERE type = 'table' AND name = 'usage_sources'
  AND sql LIKE '%CHECK (kind IN (%'
  AND sql NOT LIKE '%''opencode_v2_session''%';
-- +goose StatementEnd
-- +goose StatementBegin
PRAGMA writable_schema = RESET;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
PRAGMA writable_schema = ON;
-- +goose StatementEnd
-- +goose StatementBegin
DELETE FROM model_usage_events
WHERE usage_source_id IN (
    SELECT id FROM usage_sources WHERE kind = 'opencode_v2_session'
);
DELETE FROM usage_sources WHERE kind = 'opencode_v2_session';
DELETE FROM usage_bindings WHERE harness = 'opencode-v2';
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(
    sql,
    '''claude-code'', ''codex'', ''kimi'', ''opencode-v2''))',
    '''claude-code'', ''codex'', ''kimi''))'
)
WHERE type = 'table' AND name = 'usage_bindings';
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(
    sql,
    '''claude_main'', ''claude_subagent'', ''codex_rollout'', ''kimi_wire'', ''opencode_v2_session''))',
    '''claude_main'', ''claude_subagent'', ''codex_rollout'', ''kimi_wire''))'
)
WHERE type = 'table' AND name = 'usage_sources';
-- +goose StatementEnd
-- +goose StatementBegin
PRAGMA writable_schema = RESET;
-- +goose StatementEnd
