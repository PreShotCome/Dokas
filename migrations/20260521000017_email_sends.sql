-- +goose Up

-- +goose StatementBegin
-- One row per transactional email actually handed to the provider. It is
-- the denominator of the deliverability report (sends vs. suppressions).
-- No recipient is stored — only a timestamp — so the log carries no PII.
CREATE TABLE email_sends (
    id      BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    sent_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX email_sends_sent_at_idx ON email_sends (sent_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE email_sends;
-- +goose StatementEnd
