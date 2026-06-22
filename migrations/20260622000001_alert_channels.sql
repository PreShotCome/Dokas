-- +goose Up
-- +goose StatementBegin
-- Per-account outbound alert channels. A drill failure or a backup check-in
-- going dark is pushed to Slack (incoming webhook) and/or PagerDuty (Events
-- API v2) in addition to email + mobile push. One row per account; empty
-- string means the channel is not configured.
CREATE TABLE account_alert_channels (
    account_id            UUID PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    slack_webhook_url     TEXT NOT NULL DEFAULT '',
    pagerduty_routing_key TEXT NOT NULL DEFAULT '',
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE account_alert_channels;
-- +goose StatementEnd
