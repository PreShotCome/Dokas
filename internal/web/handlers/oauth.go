package handlers

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/preshotcome/anything/internal/analytics"
	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/auth"
	"github.com/preshotcome/anything/internal/oauth"
)

const oauthStateCookie = "so_oauth_state"

// oauthStart begins a social-login flow: it mints a CSRF state, stashes it in
// a short-lived cookie, and redirects to the provider's consent screen.
func (h *Handlers) oauthStart(w http.ResponseWriter, r *http.Request) {
	provName := chi.URLParam(r, "provider")
	prov, ok := h.oauth.Get(provName)
	if !ok {
		http.NotFound(w, r)
		return
	}
	state, err := oauth.State()
	if err != nil {
		http.Error(w, "could not start sign-in", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: oauthStateCookie, Value: state, Path: "/",
		MaxAge: 600, HttpOnly: true, Secure: h.secureCookies, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, prov.AuthCodeURL(state, h.oauthCallbackURL(r, provName)), http.StatusSeeOther)
}

// oauthCallback completes a social-login flow: it verifies the CSRF state,
// exchanges the code for the user's verified email, and signs them in —
// provisioning a user + account on first sign-in.
func (h *Handlers) oauthCallback(w http.ResponseWriter, r *http.Request) {
	provName := chi.URLParam(r, "provider")
	prov, ok := h.oauth.Get(provName)
	if !ok {
		http.NotFound(w, r)
		return
	}
	// CSRF: the state echoed back must match the cookie set at /start.
	cookie, err := r.Cookie(oauthStateCookie)
	state := r.URL.Query().Get("state")
	if err != nil || cookie.Value == "" || state == "" ||
		subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		http.Error(w, "invalid or expired sign-in state — please try again", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oauthStateCookie, Value: "", Path: "/", MaxAge: -1})

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "sign-in was cancelled", http.StatusBadRequest)
		return
	}
	id, err := prov.Identity(r.Context(), code, h.oauthCallbackURL(r, provName))
	if err != nil {
		h.logger().Warn("oauth identity failed", "provider", provName, "err", err)
		http.Error(w, "could not complete sign-in with "+provName, http.StatusBadGateway)
		return
	}
	if !id.Verified {
		http.Error(w, "your "+provName+" email address is not verified", http.StatusForbidden)
		return
	}

	// A staff SSO step-up re-proves identity for an already-signed-in staff
	// user — it is not a normal sign-in, so it branches off here.
	if c, err := r.Cookie(staffSSOCookie); err == nil && c.Value == "1" {
		http.SetCookie(w, &http.Cookie{Name: staffSSOCookie, Value: "", Path: "/", MaxAge: -1})
		h.completeStaffSSO(w, r, provName, id.Email)
		return
	}

	userID, mfaEnabled, err := h.findOrCreateOAuthUser(r.Context(),
		strings.ToLower(strings.TrimSpace(id.Email)))
	if err != nil {
		h.logger().Error("oauth sign-in failed", "provider", provName, "err", err)
		http.Error(w, "could not sign you in", http.StatusInternalServerError)
		return
	}

	// MFA still applies — OAuth replaces the password, not the second factor.
	if mfaEnabled {
		if err := h.sessions.CreatePending(r.Context(), w, userID); err != nil {
			http.Error(w, "session error", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/login/mfa", http.StatusSeeOther)
		return
	}
	currentAccount := h.pickDefaultAccount(r.Context(), userID)
	if err := h.sessions.Create(r.Context(), w, userID, currentAccount); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: nilIfZero(currentAccount), ActorID: &userID, Action: "login.succeeded",
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
		Metadata: map[string]any{"method": "oauth", "provider": provName},
	})
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *Handlers) oauthCallbackURL(r *http.Request, provName string) string {
	return absoluteURL(r, "/auth/"+provName+"/callback")
}

// completeStaffSSO finishes an admin-panel step-up. The SSO identity must
// belong to the already-signed-in staff user and still be on the staff
// allowlist — so a step-up doubles as a live re-check of staff eligibility.
func (h *Handlers) completeStaffSSO(w http.ResponseWriter, r *http.Request, provName, ssoEmail string) {
	u, ok := auth.FromContext(r.Context())
	ssoEmail = strings.ToLower(strings.TrimSpace(ssoEmail))
	if !ok || !u.IsStaff || !strings.EqualFold(u.Email, ssoEmail) || !h.staffEmails[ssoEmail] {
		http.Error(w, "staff verification failed — that account is not an authorised staff identity",
			http.StatusForbidden)
		return
	}
	if err := h.sessions.MarkStaffVerified(r.Context(), r); err != nil {
		http.Error(w, "could not record verification", http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		ActorID: &u.ID, Action: "staff.sso_verified",
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
		Metadata: map[string]any{"provider": provName},
	})
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// findOrCreateOAuthUser resolves an OAuth email to a user, provisioning a new
// user + personal account on first sign-in. The email is provider-verified,
// so a new user starts email-verified.
func (h *Handlers) findOrCreateOAuthUser(ctx context.Context, email string) (uuid.UUID, bool, error) {
	var (
		id         uuid.UUID
		mfaEnabled bool
	)
	err := h.pool.QueryRow(ctx, `
		SELECT id, mfa_enabled FROM users WHERE email = $1 AND deleted_at IS NULL
	`, email).Scan(&id, &mfaEnabled)
	if err == nil {
		return id, mfaEnabled, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, err
	}

	// First sign-in: create the user with an unusable random password — they
	// authenticate via OAuth (or a magic link), never a password.
	random, err := oauth.State()
	if err != nil {
		return uuid.Nil, false, err
	}
	hash, err := auth.HashPassword(random)
	if err != nil {
		return uuid.Nil, false, err
	}
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, email_verified)
		VALUES ($1, $2, TRUE)
		RETURNING id
	`, email, hash).Scan(&id); err != nil {
		return uuid.Nil, false, err
	}
	if h.staffEmails[email] {
		_, _ = h.pool.Exec(ctx, `UPDATE users SET is_staff = TRUE WHERE id = $1`, id)
	}
	acct, err := h.accounts.CreatePersonalAccount(ctx, id, email)
	if err != nil {
		return uuid.Nil, false, err
	}
	_ = h.audit.Record(ctx, audit.Event{
		AccountID: &acct.ID, ActorID: &id, Action: "user.signed_up",
		TargetKind: "account", TargetID: acct.ID.String(),
		Metadata: map[string]any{"method": "oauth"},
	})
	if h.billing.Enabled() {
		if cid, cErr := h.billing.Create(ctx, acct.ID, email, acct.Name); cErr == nil && cid != "" {
			_ = h.accounts.SetStripeCustomerID(ctx, acct.ID, cid)
		}
	}
	h.analytics.Capture(id.String(), analytics.EventSignedUp,
		map[string]any{"account_id": acct.ID.String()})
	return id, false, nil
}
