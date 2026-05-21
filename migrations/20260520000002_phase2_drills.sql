-- +goose Up
-- +goose StatementBegin
CREATE TABLE database_targets (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name                TEXT NOT NULL,
    source_kind         TEXT NOT NULL CHECK (source_kind IN ('postgres_dump_local')),
    source_uri          TEXT NOT NULL,
    assertion_table     TEXT NOT NULL,
    assertion_min_rows  INTEGER NOT NULL DEFAULT 1 CHECK (assertion_min_rows >= 0),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at          TIMESTAMPTZ
);
CREATE INDEX database_targets_user_id_idx ON database_targets (user_id) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE drills (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_id       UUID NOT NULL REFERENCES database_targets(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending','running','succeeded','failed')),
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    error           TEXT,
    evidence_path   TEXT,
    sandbox_db      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX drills_target_started_idx ON drills (target_id, started_at DESC NULLS LAST);
CREATE INDEX drills_user_created_idx   ON drills (user_id, created_at DESC);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE drill_steps (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    drill_id         UUID NOT NULL REFERENCES drills(id) ON DELETE CASCADE,
    name             TEXT NOT NULL
                     CHECK (name IN ('provision','fetch','restore','assert','report','teardown')),
    status           TEXT NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending','running','succeeded','failed','skipped')),
    started_at       TIMESTAMPTZ,
    completed_at     TIMESTAMPTZ,
    error            TEXT,
    idempotency_key  TEXT NOT NULL,
    ordinal          INTEGER NOT NULL,
    UNIQUE (drill_id, name),
    UNIQUE (idempotency_key)
);
CREATE INDEX drill_steps_drill_idx ON drill_steps (drill_id, ordinal);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE assertion_results (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    drill_id  UUID NOT NULL REFERENCES drills(id) ON DELETE CASCADE,
    kind      TEXT NOT NULL CHECK (kind IN ('row_count')),
    expected  JSONB NOT NULL,
    actual    JSONB NOT NULL,
    passed    BOOLEAN NOT NULL,
    at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX assertion_results_drill_idx ON assertion_results (drill_id);
-- +goose StatementEnd

-- +goose StatementBegin
-- Idempotency keys for state-changing endpoints. Scoped per user.
CREATE TABLE idempotency_keys (
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    scope      TEXT NOT NULL,
    target_id  TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, key, scope)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS assertion_results;
DROP TABLE IF EXISTS drill_steps;
DROP TABLE IF EXISTS drills;
DROP TABLE IF EXISTS database_targets;
-- +goose StatementEnd
