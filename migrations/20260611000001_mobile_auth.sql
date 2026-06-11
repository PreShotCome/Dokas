-- +goose Up

-- +goose StatementBegin
-- Bearer tokens for the native mobile (responder) app. Like api_keys, only a
-- SHA-256 hash is stored; the raw token is returned once at /mobile/login.
-- A token is bound to a single user AND account — the app is account-scoped
-- and read-only, gated by the user's membership role.
CREATE TABLE mobile_auth_tokens (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id    UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    token_prefix  TEXT NOT NULL,
    token_hash    TEXT NOT NULL UNIQUE,
    device_name   TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at  TIMESTAMPTZ,
    expires_at    TIMESTAMPTZ NOT NULL,
    revoked_at    TIMESTAMPTZ
);
CREATE INDEX mobile_auth_tokens_user_idx ON mobile_auth_tokens (user_id) WHERE revoked_at IS NULL;
CREATE INDEX mobile_auth_tokens_account_idx ON mobile_auth_tokens (account_id) WHERE revoked_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
-- Short-lived challenges bridging /mobile/login (password OK, but MFA owed)
-- and /mobile/mfa-verify. Consumed (deleted) on success; rows past expires_at
-- are dead and ignored, swept by retention.
CREATE TABLE mobile_mfa_challenges (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id  UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    device_name TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX mobile_mfa_challenges_expires_idx ON mobile_mfa_challenges (expires_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS mobile_mfa_challenges;
DROP TABLE IF EXISTS mobile_auth_tokens;
-- +goose StatementEnd
