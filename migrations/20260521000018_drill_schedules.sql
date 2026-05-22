-- +goose Up
-- Scheduled drills: a target can carry a recurring drill cadence. The
-- scheduler (a River periodic job) enqueues a drill whenever next_drill_at
-- is due, then advances it by the cadence interval.
ALTER TABLE database_targets
    ADD COLUMN drill_cadence text NOT NULL DEFAULT 'off'
        CHECK (drill_cadence IN ('off', 'weekly', 'daily', 'hourly')),
    ADD COLUMN next_drill_at timestamptz;

-- Partial index over the rows the scheduler actually scans.
CREATE INDEX idx_database_targets_due
    ON database_targets (next_drill_at)
    WHERE drill_cadence <> 'off' AND deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_database_targets_due;
ALTER TABLE database_targets
    DROP COLUMN next_drill_at,
    DROP COLUMN drill_cadence;
