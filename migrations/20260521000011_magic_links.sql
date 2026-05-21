-- +goose Up

-- +goose StatementBegin
-- One-time tokens for passwordless ("magic link") sign-in. Only the SHA-256
-- hash is stored; the token lives in the emailed URL.
CREATE TABLE magic_link_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX magic_link_tokens_user_idx ON magic_link_tokens (user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS magic_link_tokens;
-- +goose StatementEnd
