-- Per-puller acknowledgement state for reports pulled from this worker by a
-- peer daemon. A report stays pending for a puller until that puller
-- acknowledges it, so several daemons can each receive every report.
-- +goose Up
CREATE TABLE report_pull_acks (
    report_id TEXT NOT NULL REFERENCES reports (id) ON DELETE CASCADE,
    puller_id TEXT NOT NULL CHECK (length(puller_id) BETWEEN 1 AND 128),
    acknowledged_at TIMESTAMP NOT NULL,
    PRIMARY KEY (report_id, puller_id)
);
CREATE INDEX idx_report_pull_acks_puller ON report_pull_acks (puller_id, report_id);

-- One row per (worker, puller) that has pulled. expired_count is the number of
-- reports that were compacted before this puller acknowledged them. The
-- puller reads it on its next pull and passes it back as expiredSeen.
CREATE TABLE report_pullers (
    session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    puller_id TEXT NOT NULL CHECK (length(puller_id) BETWEEN 1 AND 128),
    expired_count INTEGER NOT NULL DEFAULT 0 CHECK (expired_count >= 0),
    last_pulled_at TIMESTAMP NOT NULL,
    PRIMARY KEY (session_id, puller_id)
);

-- +goose Down
DROP TABLE report_pullers;
DROP INDEX idx_report_pull_acks_puller;
DROP TABLE report_pull_acks;
