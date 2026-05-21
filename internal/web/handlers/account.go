package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/preshotcome/anything/internal/account"
	"github.com/preshotcome/anything/internal/analytics"
	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/auth"
	mail "github.com/preshotcome/anything/internal/email"
	"github.com/preshotcome/anything/internal/web/templates"
)

// --- /account ---

func (h *Handlers) accountSettings(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	members, _ := h.accounts.ListMembers(r.Context(), lc.Account.ID)
	pending, _ := h.accounts.ListPendingInvitations(r.Context(), lc.Account.ID)
	render(w, r, templates.AccountSettings(lc, members, pending, h.billing.Enabled()))
}

// --- /account/invitations ---

func (h *Handlers) inviteCreate(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(strings.ToLower(r.PostFormValue("email")))
	roleStr := strings.TrimSpace(r.PostFormValue("role"))
	role, err := account.ParseRole(roleStr)
	if err != nil || !role.ValidInviteRole() {
		http.Error(w, "invalid role (use admin|member|viewer)", http.StatusBadRequest)
		return
	}
	if email == "" || !strings.Contains(email, "@") {
		http.Error(w, "invalid email", http.StatusBadRequest)
		return
	}

	rawToken, inv, err := h.accounts.CreateInvitation(r.Context(), acct.ID, u.ID, email, role, 7*24*time.Hour)
	if err != nil {
		http.Error(w, "invite: "+err.Error(), http.StatusInternalServerError)
		return
	}

	link := absoluteURL(r, "/invitations/"+rawToken)
	// Send the invitation email. With LogMailer (no POSTMARK_TOKEN) this
	// logs the rendered message — including the link — so local dev can
	// still copy-paste it, exactly as the Phase 3 stdout log did.
	if err := h.mailer.Send(r.Context(), mail.InvitationMessage(email, acct.Name, string(role), link)); err != nil &&
		!errors.Is(err, mail.ErrSuppressed) {
		h.logger().Warn("invitation email failed", "to", email, "err", err)
	}

	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &acct.ID, ActorID: &u.ID, Action: "account.invited",
		TargetKind: "invitation", TargetID: inv.ID.String(),
		Metadata: map[string]any{"email": email, "role": string(role)},
	})
	h.analytics.Capture(u.ID.String(), analytics.EventInvitationSent, map[string]any{
		"account_id": acct.ID.String(),
		"role":       string(role),
	})

	http.Redirect(w, r, "/account?invited="+inv.ID.String(), http.StatusSeeOther)
}

// --- /invitations/{token} ---

func (h *Handlers) invitationPage(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	inv, err := h.accounts.LookupInvitation(r.Context(), token)
	if err != nil {
		if errors.Is(err, account.ErrNotFound) {
			http.Error(w, "invitation not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, account.ErrInvitationGone) {
			render(w, r, templates.InvitationGone())
			return
		}
		http.Error(w, "lookup: "+err.Error(), http.StatusInternalServerError)
		return
	}
	acct, err := h.accounts.GetAccount(r.Context(), inv.AccountID)
	if err != nil {
		http.Error(w, "account gone", http.StatusNotFound)
		return
	}
	u, isLoggedIn := auth.FromContext(r.Context())
	render(w, r, templates.InvitationAccept(u, isLoggedIn, acct, inv, token))
}

func (h *Handlers) invitationAccept(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.FromContext(r.Context())
	if !ok {
		// Not logged in — bounce to signup with a return-to.
		token := chi.URLParam(r, "token")
		http.Redirect(w, r, "/signup?next=/invitations/"+token, http.StatusSeeOther)
		return
	}
	token := chi.URLParam(r, "token")

	inv, err := h.accounts.AcceptInvitation(r.Context(), token, u.ID)
	if err != nil {
		if errors.Is(err, account.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, account.ErrInvitationGone) {
			render(w, r, templates.InvitationGone())
			return
		}
		http.Error(w, "accept: "+err.Error(), http.StatusInternalServerError)
		return
	}

	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &inv.AccountID, ActorID: &u.ID, Action: "account.member_added",
		TargetKind: "user", TargetID: u.ID.String(),
		Metadata: map[string]any{"via": "invitation", "role": string(inv.Role)},
	})

	// Switch to the newly joined account.
	if err := h.sessions.SetCurrentAccount(r.Context(), r, inv.AccountID); err != nil {
		http.Error(w, "switch account: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// --- /account/members ---

func (h *Handlers) memberUpdate(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	userID, err := uuid.Parse(chi.URLParam(r, "user_id"))
	if err != nil {
		http.Error(w, "invalid user_id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	role, err := account.ParseRole(r.PostFormValue("role"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.accounts.UpdateMemberRole(r.Context(), acct.ID, userID, role); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &acct.ID, ActorID: &u.ID, Action: "account.member_role_changed",
		TargetKind: "user", TargetID: userID.String(),
		Metadata: map[string]any{"role": string(role)},
	})
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

func (h *Handlers) memberRemove(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	userID, err := uuid.Parse(chi.URLParam(r, "user_id"))
	if err != nil {
		http.Error(w, "invalid user_id", http.StatusBadRequest)
		return
	}
	if err := h.accounts.RemoveMember(r.Context(), acct.ID, userID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &acct.ID, ActorID: &u.ID, Action: "account.member_removed",
		TargetKind: "user", TargetID: userID.String(),
	})
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

// --- /account/switch ---

func (h *Handlers) accountSwitch(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	target, err := uuid.Parse(r.PostFormValue("account_id"))
	if err != nil {
		http.Error(w, "invalid account_id", http.StatusBadRequest)
		return
	}

	// Must be a member.
	if _, err := h.accounts.GetMembership(r.Context(), target, u.ID); err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := h.sessions.SetCurrentAccount(r.Context(), r, target); err != nil {
		http.Error(w, "switch: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &target, ActorID: &u.ID, Action: "account.switched",
		TargetKind: "account", TargetID: target.String(),
	})
	next := r.PostFormValue("next")
	if next == "" || !strings.HasPrefix(next, "/") {
		next = "/dashboard"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func absoluteURL(r *http.Request, path string) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Host
	return scheme + "://" + host + path
}
