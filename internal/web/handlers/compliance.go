package handlers

import (
	"bytes"
	"net/http"

	"github.com/riverqueue/river"

	"github.com/preshotcome/anything/internal/account"
	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/compliance"
	"github.com/preshotcome/anything/internal/web/templates"
)

// accountExport returns the GDPR/CCPA data export as a JSON download. The
// export is built fully into memory first: if it fails we can still return a
// clean 500, rather than flushing a 200 and then a truncated body.
func (h *Handlers) accountExport(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)

	var buf bytes.Buffer
	if err := h.exporter.Export(r.Context(), lc.Account.ID, &buf); err != nil {
		h.logger().Error("account export failed", "account_id", lc.Account.ID, "err", err)
		http.Error(w, "could not generate export", http.StatusInternalServerError)
		return
	}

	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &lc.Account.ID, ActorID: &lc.User.ID, Action: "account.exported",
		TargetKind: "account", TargetID: lc.Account.ID.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
	})

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition",
		`attachment; filename="soteria-export-`+lc.Account.Slug+`.json"`)
	_, _ = w.Write(buf.Bytes())
}

// accountDelete soft-deletes the account and schedules the hard-delete job.
// Owner-only — admins can do most things but not erase the account.
func (h *Handlers) accountDelete(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	if lc.Membership == nil || lc.Membership.Role != account.RoleOwner {
		http.Error(w, "only the account owner can delete the account", http.StatusForbidden)
		return
	}

	purgeAfter, err := h.purger.SoftDelete(r.Context(), lc.Account.ID, lc.User.ID)
	if err != nil {
		http.Error(w, "delete: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Schedule the hard delete for when the grace period ends.
	if _, err := h.inserter.Insert(r.Context(),
		compliance.PurgeAccountArgs{AccountID: lc.Account.ID},
		&river.InsertOpts{ScheduledAt: purgeAfter},
	); err != nil {
		h.logger().Error("schedule account purge", "account_id", lc.Account.ID, "err", err)
	}

	// The account is now soft-deleted; the next request's LoadCurrentAccount
	// will fall the user back to another account (or none). Send them home.
	render(w, r, templates.AccountDeleted(purgeAfter))
}

// --- legal pages ---

func (h *Handlers) legalTerms(w http.ResponseWriter, r *http.Request) {
	render(w, r, templates.LegalPage(h.layoutCtx(r), "Terms of Service", templates.LegalTerms()))
}

func (h *Handlers) legalPrivacy(w http.ResponseWriter, r *http.Request) {
	render(w, r, templates.LegalPage(h.layoutCtx(r), "Privacy Policy", templates.LegalPrivacy()))
}

func (h *Handlers) legalDPA(w http.ResponseWriter, r *http.Request) {
	render(w, r, templates.LegalPage(h.layoutCtx(r), "Data Processing Addendum", templates.LegalDPA()))
}

func (h *Handlers) legalSubprocessors(w http.ResponseWriter, r *http.Request) {
	render(w, r, templates.LegalPage(h.layoutCtx(r), "Sub-processors", templates.LegalSubprocessors()))
}

func (h *Handlers) legalCookies(w http.ResponseWriter, r *http.Request) {
	render(w, r, templates.LegalPage(h.layoutCtx(r), "Cookie Policy", templates.LegalCookies()))
}
