-- ADR 0010 section 6: a coordinator summary is its own durable record, not a
-- worker report. It reuses the claim-token delivery pattern and links back to
-- the workers it covers. Rows are retained as audit history.
-- +goose Up
-- +goose StatementBegin
CREATE TABLE coordinator_summaries (
    id TEXT PRIMARY KEY,
    coordinator_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    submission_key TEXT NOT NULL CHECK (length(trim(submission_key)) > 0 AND length(submission_key) <= 200),
    scope TEXT NOT NULL CHECK (length(trim(scope)) > 0 AND length(scope) <= 200),
    state TEXT NOT NULL CHECK (state IN ('planning', 'working', 'blocked', 'done')),
    result TEXT NOT NULL DEFAULT '' CHECK (length(result) <= 2000),
    escalation_reason TEXT NOT NULL DEFAULT '' CHECK (escalation_reason IN (
        '', 'overlapping_files', 'unclear_acceptance', 'worker_failed',
        'scope_decision', 'coordinator_terminated', 'other')),
    escalation_note TEXT NOT NULL DEFAULT '' CHECK (length(escalation_note) <= 500),
    created_at TIMESTAMP NOT NULL,
    available_at TIMESTAMP NOT NULL,
    delivery_batch_id TEXT NOT NULL DEFAULT '',
    delivery_owner_id TEXT NOT NULL DEFAULT '',
    delivery_state TEXT NOT NULL DEFAULT 'pending' CHECK (delivery_state IN ('pending', 'claimed', 'acknowledged')),
    claim_token TEXT NOT NULL DEFAULT '',
    claimed_at TIMESTAMP,
    delivery_attempts INTEGER NOT NULL DEFAULT 0 CHECK (delivery_attempts >= 0),
    acknowledged_at TIMESTAMP,
    last_error TEXT NOT NULL DEFAULT '',
    UNIQUE (coordinator_session_id, submission_key),
    CHECK (escalation_reason <> 'other' OR length(trim(escalation_note)) > 0),
    CHECK (escalation_reason <> '' OR escalation_note = ''),
    CHECK (escalation_reason = '' OR state = 'blocked'),
    CHECK (
        (delivery_state = 'pending' AND claim_token = '' AND claimed_at IS NULL AND acknowledged_at IS NULL)
        OR (delivery_state = 'claimed' AND claim_token <> '' AND claimed_at IS NOT NULL AND acknowledged_at IS NULL)
        OR (delivery_state = 'acknowledged' AND claim_token <> '' AND claimed_at IS NOT NULL AND acknowledged_at IS NOT NULL)
    )
);

CREATE TABLE coordinator_summary_workers (
    summary_id TEXT NOT NULL REFERENCES coordinator_summaries(id) ON DELETE CASCADE,
    worker_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    position INTEGER NOT NULL CHECK (position >= 0),
    PRIMARY KEY (summary_id, worker_session_id)
);

CREATE INDEX idx_coordinator_summaries_delivery
    ON coordinator_summaries(delivery_state, available_at, created_at, id);
CREATE INDEX idx_coordinator_summaries_coordinator
    ON coordinator_summaries(coordinator_session_id, created_at, id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_coordinator_summaries_coordinator;
DROP INDEX IF EXISTS idx_coordinator_summaries_delivery;
DROP TABLE IF EXISTS coordinator_summary_workers;
DROP TABLE IF EXISTS coordinator_summaries;
-- +goose StatementEnd
