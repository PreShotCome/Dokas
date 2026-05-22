-- +goose Up

-- +goose StatementBegin
-- staff_verified_at records the last time the session re-proved staff
-- identity via SSO (a step-up). The admin panel requires a recent value;
-- NULL means the session has never completed an SSO step-up.
ALTER TABLE sessions
    ADD COLUMN staff_verified_at TIMESTAMPTZ;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sessions
    DROP COLUMN staff_verified_at;
-- +goose StatementEnd
