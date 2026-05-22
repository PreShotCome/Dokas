package handlers

import (
	"net/http"
	"time"

	"github.com/preshotcome/anything/internal/auth"
	"github.com/preshotcome/anything/internal/oauth"
	"github.com/preshotcome/anything/internal/web/templates"
)

// staffSSOCookie marks an OAuth flow as an admin-panel step-up rather than a
// normal sign-in. The callback consumes it.
const staffSSOCookie = "so_staff_sso"

// staffSSOWindow is how long an SSO step-up admits a session to the admin
// panel before it must re-verify.
const staffSSOWindow = time.Hour

// staffSSOProvider is the OAuth provider used as the staff identity source.
const staffSSOProvider = "google"

// staffSSOFresh reports whether a session that last verified at verifiedAt is
// still within the step-up window. A nil verifiedAt has never verified.
func staffSSOFresh(verifiedAt *time.Time, now time.Time) bool {
	return verifiedAt != nil && now.Sub(*verifiedAt) < staffSSOWindow
}

// requireStaffSSO gates the admin panel. Non-staff get a 404 (the surface is
// not acknowledged). Staff must additionally hold a recent SSO step-up — but
// only when SSO is configured; without it (dev / CI) staff status alone
// admits, as before, so those environments are not locked out.
func (h *Handlers) requireStaffSSO(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := auth.FromContext(r.Context())
		if !ok || !u.IsStaff {
			http.NotFound(w, r)
			return
		}
		if _, configured := h.oauth.Get(staffSSOProvider); !configured {
			next.ServeHTTP(w, r)
			return
		}
		verifiedAt, _ := auth.StaffVerifiedAtFromContext(r.Context())
		if staffSSOFresh(verifiedAt, time.Now()) {
			next.ServeHTTP(w, r)
			return
		}
		http.Redirect(w, r, "/admin/sso", http.StatusSeeOther)
	})
}

// adminSSOPage explains the step-up and offers the verify button.
func (h *Handlers) adminSSOPage(w http.ResponseWriter, r *http.Request) {
	_, configured := h.oauth.Get(staffSSOProvider)
	render(w, r, templates.AdminSSO(h.layoutCtx(r), configured))
}

// adminSSOStart begins a staff SSO step-up: it mints OAuth state, marks the
// flow as a step-up, and redirects to the provider's consent screen.
func (h *Handlers) adminSSOStart(w http.ResponseWriter, r *http.Request) {
	prov, ok := h.oauth.Get(staffSSOProvider)
	if !ok {
		http.Error(w, "staff SSO is not configured", http.StatusNotImplemented)
		return
	}
	state, err := oauth.State()
	if err != nil {
		http.Error(w, "could not start verification", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: oauthStateCookie, Value: state, Path: "/",
		MaxAge: 600, HttpOnly: true, Secure: h.secureCookies, SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name: staffSSOCookie, Value: "1", Path: "/",
		MaxAge: 600, HttpOnly: true, Secure: h.secureCookies, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, prov.AuthCodeURL(state, h.oauthCallbackURL(r, staffSSOProvider)), http.StatusSeeOther)
}
