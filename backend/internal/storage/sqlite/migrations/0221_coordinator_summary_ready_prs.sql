-- A coordinator summary can hand pull requests to the root as ready to
-- merge: each entry is one PR head with the approved review run for it.
-- +goose Up
-- +goose StatementBegin
CREATE TABLE coordinator_summary_ready_prs (
    summary_id TEXT NOT NULL REFERENCES coordinator_summaries(id) ON DELETE CASCADE,
    pr_number INTEGER NOT NULL CHECK (pr_number > 0),
    head_sha TEXT NOT NULL,
    review_run_id TEXT NOT NULL,
    position INTEGER NOT NULL CHECK (position >= 0),
    PRIMARY KEY (summary_id, pr_number)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE coordinator_summary_ready_prs;
-- +goose StatementEnd
