package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/auth"
	mail "github.com/preshotcome/anything/internal/email"
	"github.com/preshotcome/anything/internal/web/templates"
)

// magicLinkRequestPage shows the "email me a sign-in link" form.
func (h *Handlers) magicLinkRequestPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.FromContext(r.Context()); ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	render(w, r, templates.MagicLinkRequest(""))
}

// magicLinkRequest issues a passwordless sign-in link. The response is the
// same whether or not the email is registered, so it can't be used to
// enumerate accounts.
func (h *Handlers) magicLinkRequest(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(strings.ToLower(r.PostFormValue("email")))
	if email == "" || !strings.Contains(email, "@") {
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, templates.MagicLinkRequest("Enter a valid email."))
		return
	}

	var userID uuid.UUID
	err := h.pool.QueryRow(r.Context(), `
		SELECT id FROM users WHERE email = $1 AND deleted_at IS NULL
	`, email).Scan(&userID)
	if err == nil {
		if token, tErr := h.sessions.CreateMagicLinkToken(r.Context(), userID); tErr != nil {
			h.logger().Warn("magic link token failed", "user_id", userID, "err", tErr)
		} else {
			link := absoluteURL(r, "/login/magic/"+token)
			if mErr := h.mailer.Send(r.Context(), mail.MagicLinkMessage(email, link)); mErr != nil &&
				!errors.Is(mErr, mail.ErrSuppressed) {
				h.logger().Warn("magic link email failed", "to", email, "err", mErr)
			}
		}
	}
	// Always the same confirmation — no account enumeration.
	render(w, r, templates.MagicLinkSent(email))
}

// magicLinkConsume signs a user in from a magic-link URL. MFA, when enabled,
// still applies — the link replaces the password, not the second factor.
func (h *Handlers) magicLinkConsume(w http.ResponseWriter, r *http.Request) {
	userID, err := h.sessions.ConsumeMagicLink(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		msg := "This sign-in link is invalid."
		if errors.Is(err, auth.ErrMagicLinkGone) {
			msg = "This sign-in link has expired or was already used."
		}
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, templates.MagicLinkRequest(msg))
		return
	}

	u, err := h.sessions.LoadUserByID(r.Context(), userID)
	if err != nil {
		http.Error(w, "could not load user", http.StatusInternalServerError)
		return
	}

	// MFA users still owe a second factor — hold the session pending.
	if u.MFAEnabled {
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
		AccountID: nilIfZero(currentAccount),
		ActorID:   &userID,
		Action:    "login.succeeded",
		IP:        audit.ClientIP(r),
		UserAgent: r.UserAgent(),
	})
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}
