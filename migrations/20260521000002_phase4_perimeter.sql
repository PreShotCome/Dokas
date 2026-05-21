-- +goose Up

-- +goose StatementBegin
CREATE TABLE webhook_endpoints (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id  UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    url         TEXT NOT NULL,
    secret      TEXT NOT NULL,
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
CREATE INDEX webhook_endpoints_account_idx
    ON webhook_endpoints (account_id) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE webhook_deliveries (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    endpoint_id      UUID NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
    account_id       UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    event            TEXT NOT NULL,
    payload          JSONB NOT NULL,
    status           TEXT NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending','delivered','failed')),
    attempt_count    INTEGER NOT NULL DEFAULT 0,
    last_status_code INTEGER,
    last_error       TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    delivered_at     TIMESTAMPTZ
);
CREATE INDEX webhook_deliveries_endpoint_idx
    ON webhook_deliveries (endpoint_id, created_at DESC);
CREATE INDEX webhook_deliveries_account_idx
    ON webhook_deliveries (account_id, created_at DESC);
-- +goose StatementEnd

-- +goose StatementBegin
-- Failed-login ledger for brute-force throttling. Successful logins are also
-- recorded so a success can clear the streak. Rows are pruned by age.
CREATE TABLE login_attempts (
    id         BIGSERIAL PRIMARY KEY,
    email      CITEXT NOT NULL,
    ip         INET,
    succeeded  BOOLEAN NOT NULL,
    at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX login_attempts_email_at_idx ON login_attempts (email, at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS login_attempts;
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS webhook_endpoints;
-- +goose StatementEnd
