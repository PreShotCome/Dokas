-- +goose Up

-- +goose StatementBegin
-- Staff flag — gates the internal admin panel. No SSO yet; staff are
-- promoted from the STAFF_EMAILS allowlist at signup.
ALTER TABLE users ADD COLUMN is_staff BOOLEAN NOT NULL DEFAULT FALSE;
-- +goose StatementEnd

-- +goose StatementBegin
-- When set, the session's effective user (user_id) is being impersonated by
-- this staff member. Audit events still attribute actions to the real staff
-- actor. ON DELETE SET NULL so removing a staff user can't orphan a session.
ALTER TABLE sessions
    ADD COLUMN impersonator_user_id UUID REFERENCES users(id) ON DELETE SET NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sessions DROP COLUMN impersonator_user_id;
ALTER TABLE users DROP COLUMN is_staff;
-- +goose StatementEnd
