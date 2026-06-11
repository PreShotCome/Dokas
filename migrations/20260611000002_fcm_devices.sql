-- +goose Up

-- +goose StatementBegin
-- Registered push tokens for the mobile app. One row per device token; the
-- app re-registers (upserts) its current FCM token on launch and after a
-- token refresh. Notifications fan out to every token in the alert's account.
CREATE TABLE user_fcm_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id   UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    token        TEXT NOT NULL UNIQUE,
    platform     TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX user_fcm_tokens_account_idx ON user_fcm_tokens (account_id);
CREATE INDEX user_fcm_tokens_user_idx ON user_fcm_tokens (user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS user_fcm_tokens;
-- +goose StatementEnd
