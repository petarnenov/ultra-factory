-- name: InsertSessionStageTransition :exec
INSERT INTO session_stage_transitions (session_id, stage, entered_at)
VALUES (?, ?, ?);

-- name: ListLatestSessionStages :many
SELECT t.session_id, t.stage
FROM session_stage_transitions t
WHERE t.id = (
    SELECT MAX(l.id) FROM session_stage_transitions l WHERE l.session_id = t.session_id
);

-- name: ListSessionStageTransitions :many
SELECT stage, entered_at
FROM session_stage_transitions
WHERE session_id = ?
ORDER BY id;

-- name: ListSessionConfigHistory :many
SELECT source, harness, model, effort, recorded_at
FROM session_config_history
WHERE session_id = ?
ORDER BY id;

-- name: ListSessionUsageRuns :many
-- One row per native run root and model. Input tokens already include cached
-- input; cached and uncached are its parts, so they are never added to it.
SELECT
    ub.id AS binding_id,
    ub.harness,
    ub.native_root_id,
    ub.state AS binding_state,
    mue.model_id,
    CAST(COUNT(*) AS INTEGER) AS event_count,
    CAST(COALESCE(SUM(mue.input_tokens), 0) AS INTEGER) AS input_tokens,
    CAST(COUNT(mue.input_tokens) AS INTEGER) AS known_input_count,
    CAST(COALESCE(SUM(mue.cached_input_tokens), 0) AS INTEGER) AS cached_input_tokens,
    CAST(COUNT(mue.cached_input_tokens) AS INTEGER) AS known_cached_input_count,
    CAST(COALESCE(SUM(mue.output_tokens), 0) AS INTEGER) AS output_tokens,
    CAST(COUNT(mue.output_tokens) AS INTEGER) AS known_output_count
FROM model_usage_events mue
JOIN usage_bindings ub ON ub.id = mue.binding_id
WHERE ub.session_id = ?
GROUP BY ub.id, ub.harness, ub.native_root_id, ub.state, mue.model_id
ORDER BY ub.id, MIN(mue.id);
