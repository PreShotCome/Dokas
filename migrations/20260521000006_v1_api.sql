-- +goose Up

-- +goose StatementBegin
-- API keys authenticate /v1 requests. Only the SHA-256 hash is stored; the
-- raw key is shown to the customer once at creation. key_prefix is a short,
-- non-secret display string (e.g. "rd_a1b2c3d4").
CREATE TABLE api_keys (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id         UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name               TEXT NOT NULL,
    key_prefix         TEXT NOT NULL,
    key_hash           TEXT NOT NULL UNIQUE,
    created_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at       TIMESTAMPTZ,
    revoked_at         TIMESTAMPTZ
);
CREATE INDEX api_keys_account_idx ON api_keys (account_id) WHERE revoked_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
-- Idempotency records for state-changing /v1 requests. The stored response
-- is replayed verbatim when the same (account, key) is seen again; a key
-- reused with a different request fingerprint is rejected. Pruned after 24h
-- by the retention sweeper.
CREATE TABLE api_idempotency (
    account_id           UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    idempotency_key      TEXT NOT NULL,
    request_fingerprint  TEXT NOT NULL,
    status_code          INTEGER NOT NULL,
    response_body        BYTEA NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, idempotency_key)
);
CREATE INDEX api_idempotency_created_idx ON api_idempotency (created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS api_idempotency;
DROP TABLE IF EXISTS api_keys;
-- +goose StatementEnd
