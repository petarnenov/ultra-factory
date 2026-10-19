-- +goose Up
-- +goose StatementBegin
-- Which commit and tree a reviewer actually read. TargetSHA is the requested
-- PR head; these columns record the verified checkout so a verdict can never
-- be mistaken for a review of the worker's current working tree.
ALTER TABLE review_run ADD COLUMN reviewed_sha TEXT NOT NULL DEFAULT '';
ALTER TABLE review_run ADD COLUMN reviewed_tree TEXT NOT NULL DEFAULT '';
ALTER TABLE review_run ADD COLUMN reviewed_checkout_key TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE review_run DROP COLUMN reviewed_checkout_key;
ALTER TABLE review_run DROP COLUMN reviewed_tree;
ALTER TABLE review_run DROP COLUMN reviewed_sha;
-- +goose StatementEnd
