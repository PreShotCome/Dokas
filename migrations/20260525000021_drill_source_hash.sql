-- +goose Up
-- +goose StatementBegin
-- source_hash is the SHA-256 of the dump file Selket fetched and
-- restored — the input side of the evidence chain. Rendered in the
-- signed PDF so anyone can re-hash the dump they hold and prove it is
-- the exact bytes we drilled. NULL for drills that ran before this
-- column existed.
ALTER TABLE drills ADD COLUMN source_hash TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE drills DROP COLUMN source_hash;
-- +goose StatementEnd
