package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/preshotcome/dokaz/internal/audit"
	"github.com/preshotcome/dokaz/internal/auth"
)

// apiAlert is one entry of the responder feed: a drill outcome or a heartbeat
// liveness change, drawn from the audit log. metadata carries the event detail
// (drill_id, reason, status, …) the app uses to deep-link.
type apiAlert struct {
	ID         string         `json:"id"`
	At         time.Time      `json:"at"`
	Action     string         `json:"action"`
	TargetKind string         `json:"target_kind"`
	TargetID   string         `json:"target_id"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// v1ListAlerts returns the account's alert feed, newest first. Pagination is a
// numeric keyset cursor over the audit-event id: pass the meta.next_cursor of a
// page as ?cursor= to fetch the next, older page.
func (h *Handlers) v1ListAlerts(w http.ResponseWriter, r *http.Request) {
	acct, _ := auth.CurrentAccountFromContext(r.Context())

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	var before int64
	if v := r.URL.Query().Get("cursor"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			before = n
		}
	}

	// Fetch one extra to detect a further page.
	entries, err := h.audit.ListAlertsForAccount(r.Context(), acct.ID, limit+1, before)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "could not list alerts")
		return
	}
	meta := listMeta{}
	if len(entries) > limit {
		entries = entries[:limit]
		meta.NextCursor = strconv.FormatInt(entries[len(entries)-1].ID, 10)
	}
	out := make([]apiAlert, 0, len(entries))
	for _, e := range entries {
		out = append(out, toAPIAlert(e))
	}
	meta.Count = len(out)
	writeData(w, http.StatusOK, out, meta)
}

func toAPIAlert(e audit.Entry) apiAlert {
	return apiAlert{
		ID:         strconv.FormatInt(e.ID, 10),
		At:         e.At,
		Action:     e.Action,
		TargetKind: e.TargetKind,
		TargetID:   e.TargetID,
		Metadata:   e.Metadata,
	}
}
