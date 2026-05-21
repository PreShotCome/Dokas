-- +goose Up

-- +goose StatementBegin
-- Per-account evidence encryption keys (envelope encryption). wrapped_dek is
-- the account's 32-byte data key, itself encrypted under the server master
-- key. Evidence PDFs are encrypted with the data key; destroying this row
-- crypto-shreds the account's evidence — the ciphertext is then permanently
-- unrecoverable. ON DELETE CASCADE means an account hard-delete shreds it.
CREATE TABLE account_evidence_keys (
    account_id  UUID PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    wrapped_dek BYTEA NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS account_evidence_keys;
-- +goose StatementEnd
