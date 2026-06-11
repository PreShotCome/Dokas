package handlers

import (
	"net/http"
	"strconv"

	"github.com/preshotcome/vesta/internal/web/templates"
)

// auditLogPageSize is how many audit rows one page shows.
const auditLogPageSize = 50

// auditLog renders the account's own audit trail: a newest-first, keyset-
// paginated view over audit_events scoped to the current account. Read-only
// and account-scoped (mounted under the ActionAccountRead group), so a
// customer can answer "who did what, when" — the question every SOC 2 or
// security review eventually asks — without us pulling logs by hand.
func (h *Handlers) auditLog(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)

	var before int64
	if v := r.URL.Query().Get("before"); v != "" {
		before, _ = strconv.ParseInt(v, 10, 64)
	}

	// Fetch one extra row to learn whether an older page exists.
	entries, err := h.audit.ListForAccount(r.Context(), lc.Account.ID, auditLogPageSize+1, before)
	if err != nil {
		h.logger().Error("list audit events", "account_id", lc.Account.ID, "err", err)
		http.Error(w, "could not load audit log", http.StatusInternalServerError)
		return
	}

	var nextBefore int64
	if len(entries) > auditLogPageSize {
		entries = entries[:auditLogPageSize]
		nextBefore = entries[len(entries)-1].ID
	}

	render(w, r, templates.AuditLogPage(lc, entries, nextBefore))
}
