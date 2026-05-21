-- +goose Up

-- +goose StatementBegin
-- Detached signature over a drill's evidence PDF. One row per drill; a
-- re-signed (regenerated) report replaces the row.
CREATE TABLE evidence_signatures (
    drill_id       UUID PRIMARY KEY REFERENCES drills(id) ON DELETE CASCADE,
    algorithm      TEXT NOT NULL,
    public_key_id  TEXT NOT NULL,
    signature      TEXT NOT NULL,           -- base64
    pdf_sha256     TEXT NOT NULL,           -- hex digest the signature covers
    signed_at      TIMESTAMPTZ NOT NULL,
    retain_until   TIMESTAMPTZ NOT NULL,    -- app-enforced Object-Lock analogue
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX evidence_signatures_retain_idx ON evidence_signatures (retain_until);
-- +goose StatementEnd

-- +goose StatementBegin
-- purge_after is when a soft-deleted account becomes eligible for hard
-- delete. NULL while the account is live.
ALTER TABLE accounts ADD COLUMN purge_after TIMESTAMPTZ;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE accounts DROP COLUMN purge_after;
DROP TABLE IF EXISTS evidence_signatures;
-- +goose StatementEnd
