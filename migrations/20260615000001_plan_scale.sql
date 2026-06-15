-- +goose Up
-- +goose StatementBegin
-- Add a Scale tier between Pro/Growth and Enterprise. Lets a customer
-- with high drill volume + many databases sit on a real self-serve tier
-- instead of jumping straight to a sales conversation.
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_plan_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_plan_check
    CHECK (plan IN ('trial','starter','pro','scale'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_plan_check;
-- Revert: any 'scale' rows demoted to 'pro' before re-applying the
-- original constraint, so the migration is reversible without a
-- check-violation. (Idempotent: UPDATE ... WHERE plan='scale'.)
UPDATE accounts SET plan = 'pro' WHERE plan = 'scale';
ALTER TABLE accounts ADD CONSTRAINT accounts_plan_check
    CHECK (plan IN ('trial','starter','pro'));
-- +goose StatementEnd
