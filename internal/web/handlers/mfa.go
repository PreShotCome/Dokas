package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/auth"
	"github.com/preshotcome/anything/internal/web/templates"
)

// mfaSetupPage shows the MFA section of account settings: an enrollment form
// when MFA is off, or a disable control when it is on.
func (h *Handlers) mfaSetupPage(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	if lc.User.MFAEnabled {
		render(w, r, templates.MFAManage(lc))
		return
	}
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		http.Error(w, "could not start MFA setup", http.StatusInternalServerError)
		return
	}
	render(w, r, templates.MFASetup(lc, secret, auth.TOTPURI(secret, lc.User.Email), ""))
}

// mfaEnable confirms an enrollment: it verifies a code against the proposed
// secret, then turns MFA on and shows the one-time recovery codes.
func (h *Handlers) mfaEnable(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	if lc.User.MFAEnabled {
		http.Redirect(w, r, "/account/mfa", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	secret := strings.TrimSpace(r.PostFormValue("secret"))
	code := strings.TrimSpace(r.PostFormValue("code"))

	if secret == "" || !auth.VerifyTOTP(secret, code, time.Now()) {
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, templates.MFASetup(lc, secret, auth.TOTPURI(secret, lc.User.Email),
			"That code didn't match. Check your authenticator app and try again."))
		return
	}

	codes, err := auth.GenerateRecoveryCodes()
	if err != nil {
		http.Error(w, "could not generate recovery codes", http.StatusInternalServerError)
		return
	}
	if err := h.sessions.ReplaceRecoveryCodes(r.Context(), lc.User.ID, codes); err != nil {
		http.Error(w, "could not store recovery codes", http.StatusInternalServerError)
		return
	}
	if err := h.sessions.EnableMFA(r.Context(), lc.User.ID, secret); err != nil {
		http.Error(w, "could not enable MFA", http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		ActorID: &lc.User.ID, Action: "mfa.enabled",
		TargetKind: "user", TargetID: lc.User.ID.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
	})
	render(w, r, templates.MFARecoveryCodes(lc, codes))
}

// mfaDisable turns MFA off and clears the secret + recovery codes.
func (h *Handlers) mfaDisable(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	if err := h.sessions.DisableMFA(r.Context(), lc.User.ID); err != nil {
		http.Error(w, "could not disable MFA", http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		ActorID: &lc.User.ID, Action: "mfa.disabled",
		TargetKind: "user", TargetID: lc.User.ID.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
	})
	http.Redirect(w, r, "/account/mfa", http.StatusSeeOther)
}

// mfaChallengePage is the second login step: it asks a password-verified but
// not-yet-authenticated session for its TOTP (or recovery) code.
func (h *Handlers) mfaChallengePage(w http.ResponseWriter, r *http.Request) {
	if _, err := h.sessions.PendingMFAUser(r.Context(), r); err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	render(w, r, templates.MFAChallenge("", safeNext(r.URL.Query().Get("next"))))
}

// mfaChallengeSubmit verifies the second-factor code and promotes the pending
// session to a fully authenticated one.
func (h *Handlers) mfaChallengeSubmit(w http.ResponseWriter, r *http.Request) {
	u, err := h.sessions.PendingMFAUser(r.Context(), r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	code := strings.TrimSpace(r.PostFormValue("code"))
	next := safeNext(r.PostFormValue("next"))

	secret, err := h.sessions.TOTPSecret(r.Context(), u.ID)
	if err != nil {
		http.Error(w, "mfa lookup failed", http.StatusInternalServerError)
		return
	}
	ok := auth.VerifyTOTP(secret, code, time.Now())
	if !ok {
		// Fall back to a single-use recovery code.
		ok, err = h.sessions.ConsumeRecoveryCode(r.Context(), u.ID, code)
		if err != nil {
			http.Error(w, "mfa lookup failed", http.StatusInternalServerError)
			return
		}
	}
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		render(w, r, templates.MFAChallenge("That code is not valid. Try again or use a recovery code.", next))
		return
	}

	if err := h.sessions.CompleteMFA(r.Context(), r); err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		ActorID: &u.ID, Action: "login.succeeded",
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
	})
	http.Redirect(w, r, redirectTarget(next), http.StatusSeeOther)
}
