-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           CITEXT NOT NULL,
    email_verified  BOOLEAN NOT NULL DEFAULT FALSE,
    password_hash   TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);
CREATE UNIQUE INDEX users_email_unique ON users (email) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE sessions (
    id            UUID PRIMARY KEY,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash    TEXT NOT NULL UNIQUE,
    created_at    TIMESTAMPTZ NOT NULL,
    last_seen_at  TIMESTAMPTZ NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL
);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE audit_events (
    id          BIGSERIAL PRIMARY KEY,
    at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_id    UUID,
    action      TEXT NOT NULL,
    target_kind TEXT,
    target_id   TEXT,
    ip          INET,
    user_agent  TEXT,
    metadata    JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX audit_events_at_idx ON audit_events (at DESC);
CREATE INDEX audit_events_actor_idx ON audit_events (actor_id, at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
-- +goose StatementEnd
