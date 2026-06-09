-- +goose Up
-- +goose StatementBegin
-- The assertion_results.kind CHECK constraint was created allowing only
-- 'row_count', but the assertion engine has since grown to support
-- table_exists, column_exists, and no_nulls. Inserts of the new kinds
-- were failing the check, causing the assert step to crash even though
-- the assertion itself ran fine. Broaden the constraint to the full
-- set the engine actually emits.
ALTER TABLE assertion_results
    DROP CONSTRAINT assertion_results_kind_check;
ALTER TABLE assertion_results
    ADD CONSTRAINT assertion_results_kind_check
    CHECK (kind IN ('row_count', 'table_exists', 'column_exists', 'no_nulls'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE assertion_results
    DROP CONSTRAINT assertion_results_kind_check;
ALTER TABLE assertion_results
    ADD CONSTRAINT assertion_results_kind_check
    CHECK (kind IN ('row_count'));
-- +goose StatementEnd
