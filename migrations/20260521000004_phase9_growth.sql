-- +goose Up

-- +goose StatementBegin
-- Addresses that bounced or filed a spam complaint. The mailer checks this
-- before every send so we stop emailing addresses that hurt deliverability.
CREATE TABLE email_suppressions (
    email         CITEXT PRIMARY KEY,
    reason        TEXT NOT NULL,
    detail        TEXT,
    suppressed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS email_suppressions;
-- +goose StatementEnd
