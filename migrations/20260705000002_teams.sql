-- +goose Up

-- Teams partition an account (org) into sub-groups. A team is account-scoped
-- and soft-deletable; the name is unique per account (case-insensitive).
-- +goose StatementBegin
CREATE TABLE teams (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id  UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name        CITEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
CREATE UNIQUE INDEX teams_account_name_unique ON teams (account_id, name) WHERE deleted_at IS NULL;
CREATE INDEX teams_account_idx ON teams (account_id) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- team_memberships is a many-to-many user↔team join within an account. A user
-- can sit on several teams. Rows cascade when either side is deleted.
-- +goose StatementBegin
CREATE TABLE team_memberships (
    team_id     UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, user_id)
);
CREATE INDEX team_memberships_user_idx ON team_memberships (user_id);
-- +goose StatementEnd

-- A database belongs to at most one team. NULL = unassigned = account-wide
-- visibility (every existing database backfills to NULL, so the feature is
-- inert until an admin assigns one). ON DELETE SET NULL: deleting a team
-- returns its databases to account-wide rather than orphaning them.
-- +goose StatementBegin
ALTER TABLE database_targets ADD COLUMN team_id UUID REFERENCES teams(id) ON DELETE SET NULL;
CREATE INDEX database_targets_team_idx ON database_targets (team_id)
    WHERE deleted_at IS NULL AND team_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS database_targets_team_idx;
ALTER TABLE database_targets DROP COLUMN IF EXISTS team_id;
DROP TABLE IF EXISTS team_memberships;
DROP TABLE IF EXISTS teams;
-- +goose StatementEnd
