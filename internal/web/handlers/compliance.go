package handlers

import (
	"bytes"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/preshotcome/anything/internal/account"
	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/auth"
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
		`attachment; filename="selket-export-`+lc.Account.Slug+`.json"`)
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

// v1DeleteAccount is the programmatic GDPR right-to-erasure endpoint:
// DELETE /v1/accounts/{id}. It soft-deletes the API key's own account now
// and schedules the hard delete (crypto-shred + row cascade) for when the
// grace window closes — the same lifecycle the browser button drives. The
// {id} in the path MUST match the authenticated account: a key can only
// erase the account it belongs to. Gated on the opt-in account:delete
// scope; never paywalled on trial state.
func (h *Handlers) v1DeleteAccount(w http.ResponseWriter, r *http.Request) {
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	key, _ := apiKeyFromContext(r.Context())

	pathID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid account id")
		return
	}
	if pathID != acct.ID {
		// Don't leak whether the other account exists.
		writeAPIError(w, http.StatusForbidden, "forbidden",
			"an API key can only delete the account it belongs to")
		return
	}

	purgeAfter, err := h.purger.SoftDelete(r.Context(), acct.ID, key.CreatedByUserID)
	if err != nil {
		// Most often: already soft-deleted. Idempotency middleware replays
		// exact retries; a fresh call against an already-deleting account
		// reads as a conflict, not a server fault.
		h.logger().Warn("v1 account delete soft-delete failed",
			"account_id", acct.ID, "err", err)
		writeAPIError(w, http.StatusConflict, "already_deleted",
			"account is already scheduled for deletion")
		return
	}

	if _, err := h.inserter.Insert(r.Context(),
		compliance.PurgeAccountArgs{AccountID: acct.ID},
		&river.InsertOpts{ScheduledAt: purgeAfter},
	); err != nil {
		h.logger().Error("schedule account purge (v1)", "account_id", acct.ID, "err", err)
	}

	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &acct.ID, ActorID: &key.CreatedByUserID, Action: "account.delete_requested",
		TargetKind: "account", TargetID: acct.ID.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
		Metadata: map[string]any{"via": "v1", "api_key_id": key.ID.String()},
	})

	writeData(w, http.StatusAccepted, map[string]any{
		"id":          acct.ID,
		"status":      "deletion_scheduled",
		"purge_after": purgeAfter.UTC().Format(time.RFC3339),
	}, nil)
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
