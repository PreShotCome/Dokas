-- +goose Up

-- +goose StatementBegin
CREATE TABLE accounts (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                TEXT NOT NULL,
    slug                CITEXT NOT NULL,
    stripe_customer_id  TEXT,
    plan                TEXT NOT NULL DEFAULT 'trial'
                        CHECK (plan IN ('trial','starter','pro')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at          TIMESTAMPTZ
);
CREATE UNIQUE INDEX accounts_slug_unique ON accounts (slug) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE memberships (
    account_id  UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id)    ON DELETE CASCADE,
    role        TEXT NOT NULL CHECK (role IN ('owner','admin','member','viewer')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, user_id)
);
CREATE INDEX memberships_user_idx ON memberships (user_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE invitations (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id          UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    email               CITEXT NOT NULL,
    role                TEXT NOT NULL CHECK (role IN ('admin','member','viewer')),
    token_hash          TEXT NOT NULL UNIQUE,
    invited_by_user_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at          TIMESTAMPTZ NOT NULL,
    accepted_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX invitations_account_idx ON invitations (account_id);
CREATE INDEX invitations_email_pending_idx ON invitations (email) WHERE accepted_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE sessions ADD COLUMN current_account_id UUID REFERENCES accounts(id) ON DELETE SET NULL;
-- +goose StatementEnd

-- Backfill: every existing user gets one personal account where they are owner.
-- +goose StatementBegin
INSERT INTO accounts (id, name, slug)
SELECT
    gen_random_uuid(),
    split_part(email::text, '@', 1) || '''s workspace',
    lower(regexp_replace(split_part(email::text, '@', 1), '[^a-zA-Z0-9]+', '-', 'g'))
        || '-' || substr(replace(id::text, '-', ''), 1, 6)
FROM users
WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
-- Pair each new account back to its owning user via the slug suffix we just
-- generated (the trailing 6 chars of the user UUID). Single-statement so
-- the migration is reversible without a procedural block.
INSERT INTO memberships (account_id, user_id, role)
SELECT a.id, u.id, 'owner'
  FROM accounts a
  JOIN users u
    ON a.slug LIKE '%-' || substr(replace(u.id::text, '-', ''), 1, 6)
 WHERE a.deleted_at IS NULL AND u.deleted_at IS NULL;
-- +goose StatementEnd

-- Add account scoping to drill-domain tables, then backfill from
-- memberships, then make NOT NULL.
-- +goose StatementBegin
ALTER TABLE database_targets ADD COLUMN account_id UUID REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE drills           ADD COLUMN account_id UUID REFERENCES accounts(id) ON DELETE CASCADE;

UPDATE database_targets t
   SET account_id = m.account_id
  FROM memberships m
 WHERE m.user_id = t.user_id AND m.role = 'owner';

UPDATE drills d
   SET account_id = m.account_id
  FROM memberships m
 WHERE m.user_id = d.user_id AND m.role = 'owner';

ALTER TABLE database_targets ALTER COLUMN account_id SET NOT NULL;
ALTER TABLE drills           ALTER COLUMN account_id SET NOT NULL;

ALTER TABLE database_targets RENAME COLUMN user_id TO created_by_user_id;
ALTER TABLE drills           RENAME COLUMN user_id TO created_by_user_id;

CREATE INDEX database_targets_account_idx ON database_targets (account_id) WHERE deleted_at IS NULL;
CREATE INDEX drills_account_created_idx   ON drills (account_id, created_at DESC);
-- +goose StatementEnd

-- Idempotency keys move from per-user to per-account scope (an account-level
-- form submission shouldn't be deduped by which member clicked submit).
-- +goose StatementBegin
ALTER TABLE idempotency_keys
    DROP CONSTRAINT idempotency_keys_pkey,
    ADD COLUMN account_id UUID REFERENCES accounts(id) ON DELETE CASCADE;

UPDATE idempotency_keys k
   SET account_id = m.account_id
  FROM memberships m
 WHERE m.user_id = k.user_id AND m.role = 'owner';

ALTER TABLE idempotency_keys ALTER COLUMN account_id SET NOT NULL;
ALTER TABLE idempotency_keys ADD PRIMARY KEY (account_id, key, scope);
-- +goose StatementEnd

-- Audit log gains account scope so customers can view their own audit feed.
-- Existing rows stay NULL (they pre-date accounts and were not customer-facing).
-- +goose StatementBegin
ALTER TABLE audit_events ADD COLUMN account_id UUID REFERENCES accounts(id) ON DELETE SET NULL;
CREATE INDEX audit_events_account_at_idx ON audit_events (account_id, at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS audit_events_account_at_idx;
ALTER TABLE audit_events DROP COLUMN account_id;

ALTER TABLE idempotency_keys DROP CONSTRAINT idempotency_keys_pkey;
ALTER TABLE idempotency_keys DROP COLUMN account_id;
ALTER TABLE idempotency_keys ADD PRIMARY KEY (user_id, key, scope);

ALTER TABLE drills           RENAME COLUMN created_by_user_id TO user_id;
ALTER TABLE database_targets RENAME COLUMN created_by_user_id TO user_id;
DROP INDEX IF EXISTS drills_account_created_idx;
DROP INDEX IF EXISTS database_targets_account_idx;
ALTER TABLE drills           DROP COLUMN account_id;
ALTER TABLE database_targets DROP COLUMN account_id;

ALTER TABLE sessions DROP COLUMN current_account_id;

DROP TABLE IF EXISTS invitations;
DROP TABLE IF EXISTS memberships;
DROP TABLE IF EXISTS accounts;
-- +goose StatementEnd
