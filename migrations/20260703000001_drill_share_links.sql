-- +goose Up
-- +goose StatementBegin
-- Tokenized share links for a single drill: an auditor can view the receipt,
-- the signature, and verify the PDF without needing a Dokaz account. Tokens
-- are hashed at rest (never stored in cleartext) and expire.
CREATE TABLE drill_share_links (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    drill_id      UUID NOT NULL REFERENCES drills(id) ON DELETE CASCADE,
    account_id    UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    created_by    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash    BYTEA NOT NULL,
    token_prefix  TEXT NOT NULL,             -- for admin listing / audit
    label         TEXT NOT NULL DEFAULT '',  -- optional human name ("Q3 audit / K. Patel")
    expires_at    TIMESTAMPTZ NOT NULL,
    revoked_at    TIMESTAMPTZ,
    last_viewed_at TIMESTAMPTZ,
    view_count    INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT drill_share_links_token_hash_uniq UNIQUE (token_hash)
);
CREATE INDEX drill_share_links_drill_id ON drill_share_links (drill_id);
CREATE INDEX drill_share_links_account_id ON drill_share_links (account_id, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE drill_share_links;
-- +goose StatementEnd
