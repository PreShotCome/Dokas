-- +goose Up

-- +goose StatementBegin
-- TOTP MFA: the shared secret (base32) and whether MFA is active. The secret
-- is only set once a user confirms enrollment with a valid code.
ALTER TABLE users
    ADD COLUMN totp_secret TEXT,
    ADD COLUMN mfa_enabled BOOLEAN NOT NULL DEFAULT FALSE;
-- +goose StatementEnd

-- +goose StatementBegin
-- A session created after a correct password but before the MFA code is
-- mfa_pending: it exists but does not authenticate app requests.
ALTER TABLE sessions
    ADD COLUMN mfa_pending BOOLEAN NOT NULL DEFAULT FALSE;
-- +goose StatementEnd

-- +goose StatementBegin
-- Single-use recovery codes for when an authenticator device is lost. Only
-- the hash is stored, mirroring sessions and invitations.
CREATE TABLE mfa_recovery_codes (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash  TEXT NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX mfa_recovery_codes_user_idx ON mfa_recovery_codes (user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS mfa_recovery_codes;
ALTER TABLE sessions DROP COLUMN mfa_pending;
ALTER TABLE users DROP COLUMN totp_secret, DROP COLUMN mfa_enabled;
-- +goose StatementEnd
