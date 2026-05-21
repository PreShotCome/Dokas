-- +goose Up

-- +goose StatementBegin
-- A target carries many assertions, each a typed check with a JSON config.
-- The assert step runs every one against the restored sandbox.
CREATE TABLE assertions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_id  UUID NOT NULL REFERENCES database_targets(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL
               CHECK (kind IN ('row_count','table_exists','column_exists','no_nulls')),
    config     JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX assertions_target_idx ON assertions (target_id);
-- +goose StatementEnd

-- +goose StatementBegin
-- Migrate each target's single baked-in row_count assertion into the table.
INSERT INTO assertions (target_id, kind, config)
SELECT id, 'row_count',
       jsonb_build_object('table', assertion_table, 'min_rows', assertion_min_rows)
  FROM database_targets;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE database_targets
    DROP COLUMN assertion_table,
    DROP COLUMN assertion_min_rows;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE database_targets
    ADD COLUMN assertion_table    TEXT,
    ADD COLUMN assertion_min_rows INTEGER NOT NULL DEFAULT 1;

-- Restore the first row_count assertion per target back onto the columns.
UPDATE database_targets t
   SET assertion_table    = a.config->>'table',
       assertion_min_rows = COALESCE((a.config->>'min_rows')::int, 1)
  FROM (
    SELECT DISTINCT ON (target_id) target_id, config
      FROM assertions WHERE kind = 'row_count'
     ORDER BY target_id, created_at
  ) a
 WHERE a.target_id = t.id;

UPDATE database_targets SET assertion_table = 'events' WHERE assertion_table IS NULL;
ALTER TABLE database_targets ALTER COLUMN assertion_table SET NOT NULL;

DROP TABLE IF EXISTS assertions;
-- +goose StatementEnd
