-- +goose Up
-- +goose StatementBegin
-- Raw output captured from a step's subprocess (pg_restore, psql,
-- etc.). Three columns intentionally:
--
--   output_snippet — first ~16 KiB of the combined stdout+stderr.
--                    Big enough to read a typical restore log, small
--                    enough to keep in the signed PDF body.
--   output_sha256  — hex SHA-256 of the FULL output (not the snippet).
--                    Even with truncation, an auditor can re-run the
--                    same tool against the same dump and confirm the
--                    captured snippet matches a prefix of the true
--                    output (and that the hash matches end-to-end).
--   output_truncated — whether the snippet stops short of the full
--                      output (true) or contains everything (false).
--
-- All three are NULL for steps that didn't produce output (fetch,
-- provision, assert, teardown today).
ALTER TABLE drill_steps
    ADD COLUMN output_snippet   TEXT,
    ADD COLUMN output_sha256    TEXT,
    ADD COLUMN output_truncated BOOLEAN;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE drill_steps
    DROP COLUMN output_snippet,
    DROP COLUMN output_sha256,
    DROP COLUMN output_truncated;
-- +goose StatementEnd
