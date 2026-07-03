-- +goose Up
-- +goose StatementBegin
-- Per-account "unlimited" flag. When true, every resource cap, cadence gate,
-- trial paywall, drill quota, dump-size check, and dunning/trial banner is
-- short-circuited for that account. Founder / staff-owned accounts use this
-- so the sole engineer isn't fighting his own product's guardrails.
ALTER TABLE accounts
    ADD COLUMN is_unlimited BOOLEAN NOT NULL DEFAULT false;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE accounts DROP COLUMN is_unlimited;
-- +goose StatementEnd
