package handlers

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/auth"
	mail "github.com/preshotcome/anything/internal/email"
	"github.com/preshotcome/anything/internal/web/templates"
)

// verifyEmail consumes a verification token from an emailed link. It is
// public — the link must work whether or not the visitor is signed in.
func (h *Handlers) verifyEmail(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	userID, err := h.sessions.VerifyEmail(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		msg := "This verification link is invalid."
		if errors.Is(err, auth.ErrVerificationGone) {
			msg = "This verification link has expired or was already used."
		}
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, templates.VerifyEmailResult(lc, false, msg))
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		ActorID: &userID, Action: "email.verified",
		TargetKind: "user", TargetID: userID.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
	})
	render(w, r, templates.VerifyEmailResult(lc, true, "Your email address is verified — thanks!"))
}

// verifyEmailResend issues a fresh verification token to the signed-in user
// and emails the link. Reached from the verify-your-email banner.
func (h *Handlers) verifyEmailResend(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	if lc.User == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if lc.User.EmailVerified {
		render(w, r, templates.VerifyEmailResult(lc, true, "Your email is already verified."))
		return
	}
	token, err := h.sessions.CreateVerificationToken(r.Context(), lc.User.ID)
	if err != nil {
		http.Error(w, "could not create verification token", http.StatusInternalServerError)
		return
	}
	link := absoluteURL(r, "/verify-email/"+token)
	if err := h.mailer.Send(r.Context(), mail.VerifyEmailMessage(lc.User.Email, link)); err != nil &&
		!errors.Is(err, mail.ErrSuppressed) {
		h.logger().Warn("verification email failed", "to", lc.User.Email, "err", err)
	}
	render(w, r, templates.VerifyEmailResult(lc, true,
		"We've sent a fresh verification link to "+lc.User.Email+"."))
}
