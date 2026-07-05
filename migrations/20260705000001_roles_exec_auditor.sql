-- +goose Up

-- Two new membership roles:
--   * exec    — internal executive dashboard access (read-only, same surface
--               as viewer). Distinguished from viewer by label + intent so a
--               "Roles: 1 owner, 2 admins, 3 execs" summary reads correctly.
--   * auditor — external auditor / compliance reviewer. Reads drills,
--               evidence, targets, heartbeats — but NOT billing and NOT the
--               member roster (privacy). Complements the public share-link
--               path for auditors who want a real logged-in account.
-- Extend the CHECK constraint on both memberships (who can hold the role)
-- and invitations (who can be invited into it) — RoleOwner stays
-- invitation-forbidden.

-- +goose StatementBegin
ALTER TABLE memberships DROP CONSTRAINT memberships_role_check;
ALTER TABLE memberships ADD CONSTRAINT memberships_role_check
    CHECK (role IN ('owner','admin','member','viewer','exec','auditor'));
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE invitations DROP CONSTRAINT invitations_role_check;
ALTER TABLE invitations ADD CONSTRAINT invitations_role_check
    CHECK (role IN ('admin','member','viewer','exec','auditor'));
-- +goose StatementEnd


-- +goose Down

-- Down migration collapses exec → viewer and auditor → viewer so the
-- rollback doesn't leave rows that violate the tightened CHECK constraint.
-- Not lossless — no way to distinguish them on the way back up — but
-- documented so the operator running the down knows what they're accepting.

-- +goose StatementBegin
UPDATE memberships SET role = 'viewer' WHERE role IN ('exec','auditor');
UPDATE invitations SET role = 'viewer' WHERE role IN ('exec','auditor');
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE memberships DROP CONSTRAINT memberships_role_check;
ALTER TABLE memberships ADD CONSTRAINT memberships_role_check
    CHECK (role IN ('owner','admin','member','viewer'));
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE invitations DROP CONSTRAINT invitations_role_check;
ALTER TABLE invitations ADD CONSTRAINT invitations_role_check
    CHECK (role IN ('admin','member','viewer'));
-- +goose StatementEnd
