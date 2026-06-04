-- +goose Up
-- Heartbeats (backup check-ins): a customer's backup cron pings a unique
-- token URL after each run. Each monitor declares an expected period plus a
-- grace window; the sweeper (a River periodic job) flips a monitor to 'down'
-- when now() passes expected_by + grace, and a ping flips it back to 'up'.
-- This is the passive complement to drills — drills actively restore-and-
-- verify; heartbeats confirm the job even ran.
CREATE TABLE heartbeats (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id          UUID NOT NULL REFERENCES accounts(id),
    created_by_user_id  UUID NOT NULL REFERENCES users(id),
    name                TEXT NOT NULL,
    slug                TEXT NOT NULL,
    ping_token          TEXT NOT NULL UNIQUE,
    period_seconds      INTEGER NOT NULL CHECK (period_seconds > 0),
    grace_seconds       INTEGER NOT NULL DEFAULT 0 CHECK (grace_seconds >= 0),
    status              TEXT NOT NULL DEFAULT 'new'
                            CHECK (status IN ('new', 'up', 'down', 'paused')),
    last_ping_at        TIMESTAMPTZ,
    -- When the next ping is due. Set on creation (created_at + period) so a
    -- monitor that never pings can still lapse; advanced to now() + period on
    -- every ping. NULL only while paused.
    expected_by         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at          TIMESTAMPTZ
);

CREATE INDEX idx_heartbeats_account ON heartbeats (account_id, created_at DESC)
    WHERE deleted_at IS NULL;

-- Partial index over exactly the rows the sweeper scans: active monitors that
-- are eligible to lapse. Paused/down/deleted rows are skipped.
CREATE INDEX idx_heartbeats_due ON heartbeats (expected_by)
    WHERE deleted_at IS NULL AND status IN ('new', 'up');

-- A rolling event log of received pings, surfaced on the monitor detail page
-- and pruned by the retention sweeper.
CREATE TABLE heartbeat_pings (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    heartbeat_id  UUID NOT NULL REFERENCES heartbeats(id) ON DELETE CASCADE,
    received_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    kind          TEXT NOT NULL DEFAULT 'ping'
                      CHECK (kind IN ('ping', 'start', 'fail')),
    source_ip     TEXT,
    user_agent    TEXT
);

CREATE INDEX idx_heartbeat_pings_hb ON heartbeat_pings (heartbeat_id, received_at DESC);

-- +goose Down
DROP TABLE IF EXISTS heartbeat_pings;
DROP TABLE IF EXISTS heartbeats;
