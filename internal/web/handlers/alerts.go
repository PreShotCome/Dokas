// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package handlers

import (
	"net/http"
	"strings"

	"github.com/preshotcome/dokaz/internal/alerting"
	"github.com/preshotcome/dokaz/internal/audit"
	"github.com/preshotcome/dokaz/internal/auth"
)

// alertChannelsSave updates the account's Slack + PagerDuty alert channels.
// Mounted under the account.write RBAC group. Empty fields keep the current
// value (so the secret never has to be re-entered); the explicit clear_*
// checkboxes remove a channel.
func (h *Handlers) alertChannelsSave(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	cur, _ := h.alerts.Get(r.Context(), lc.Account.ID)
	next := cur

	if r.PostFormValue("clear_slack") == "1" {
		next.SlackWebhookURL = ""
	} else if v := strings.TrimSpace(r.PostFormValue("slack_webhook_url")); v != "" {
		next.SlackWebhookURL = v
	}
	if r.PostFormValue("clear_pagerduty") == "1" {
		next.PagerDutyRoutingKey = ""
	} else if v := strings.TrimSpace(r.PostFormValue("pagerduty_routing_key")); v != "" {
		next.PagerDutyRoutingKey = v
	}

	// A Slack incoming webhook is always an HTTPS URL; reject anything else
	// rather than silently failing to deliver later.
	if next.SlackWebhookURL != "" && !strings.HasPrefix(next.SlackWebhookURL, "https://") {
		http.Redirect(w, r, "/account?alerts=badslack", http.StatusSeeOther)
		return
	}

	if err := h.alerts.Set(r.Context(), lc.Account.ID, alerting.Channels(next)); err != nil {
		http.Error(w, "could not save alert settings", http.StatusInternalServerError)
		return
	}
	if u, ok := auth.FromContext(r.Context()); ok {
		_ = h.audit.Record(r.Context(), audit.Event{
			AccountID: &lc.Account.ID, ActorID: &u.ID, Action: "account.alerts_updated",
			TargetKind: "account", TargetID: lc.Account.ID.String(),
			IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
		})
	}
	http.Redirect(w, r, "/account?alerts=saved", http.StatusSeeOther)
}
