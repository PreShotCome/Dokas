-- +goose Up
-- +goose StatementBegin
-- Mark a target as the built-in sample (demo) dataset vs. a customer's own
-- backup. A free/trial account may only drill the sample; drilling a real
-- target requires a paid plan. Defaults to false so every existing target
-- is treated as a real customer database.
ALTER TABLE database_targets
    ADD COLUMN is_sample BOOLEAN NOT NULL DEFAULT false;
-- At most one sample target per account (the demo is idempotent).
CREATE UNIQUE INDEX database_targets_one_sample_per_account
    ON database_targets (account_id)
    WHERE is_sample AND deleted_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS database_targets_one_sample_per_account;
ALTER TABLE database_targets
    DROP COLUMN is_sample;
-- +goose StatementEnd
