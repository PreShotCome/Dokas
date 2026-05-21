package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/preshotcome/anything/internal/account"
	"github.com/preshotcome/anything/internal/analytics"
	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/auth"
	mail "github.com/preshotcome/anything/internal/email"
	"github.com/preshotcome/anything/internal/flags"
	"github.com/preshotcome/anything/internal/web/templates"
)

func (h *Handlers) index(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.FromContext(r.Context()); ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handlers) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.FromContext(r.Context()); ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	// A session that passed the password but still owes an MFA code lands
	// here on a refresh — send it on to the challenge.
	if _, err := h.sessions.PendingMFAUser(r.Context(), r); err == nil {
		http.Redirect(w, r, "/login/mfa", http.StatusSeeOther)
		return
	}
	render(w, r, templates.LoginWithNext("", "", safeNext(r.URL.Query().Get("next"))))
}

func (h *Handlers) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(strings.ToLower(r.PostFormValue("email")))
	password := r.PostFormValue("password")
	next := safeNext(r.PostFormValue("next"))

	if email == "" || password == "" {
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, templates.LoginWithNext("Enter an email and password.", email, next))
		return
	}

	// Brute-force throttle: refuse early when this email has too many recent
	// failures, regardless of whether the submitted password is right.
	if lock, err := h.throttle.Check(r.Context(), email); err == nil && lock.Locked {
		mins := int(lock.RetryAfter.Minutes()) + 1
		w.Header().Set("Retry-After", strconv.Itoa(int(lock.RetryAfter.Seconds())+1))
		w.WriteHeader(http.StatusTooManyRequests)
		render(w, r, templates.LoginWithNext(
			"Too many failed attempts. Try again in about "+strconv.Itoa(mins)+" minute(s).",
			email, next))
		return
	}

	clientIP := audit.ClientIP(r)

	var id uuid.UUID
	var hash string
	var mfaEnabled bool
	err := h.pool.QueryRow(r.Context(), `
		SELECT id, password_hash, mfa_enabled FROM users
		 WHERE email = $1 AND deleted_at IS NULL
	`, email).Scan(&id, &hash, &mfaEnabled)
	if err != nil {
		// Constant-ish time: always run a verify against a dummy hash.
		_ = auth.VerifyPassword(password, dummyHash)
		_ = h.throttle.Record(r.Context(), email, clientIP, false)
		w.WriteHeader(http.StatusUnauthorized)
		render(w, r, templates.LoginWithNext("Invalid email or password.", email, next))
		return
	}

	if err := auth.VerifyPassword(password, hash); err != nil {
		_ = h.throttle.Record(r.Context(), email, clientIP, false)
		_ = h.audit.Record(r.Context(), audit.Event{
			Action:    "login.failed",
			TargetID:  id.String(),
			IP:        clientIP,
			UserAgent: r.UserAgent(),
		})
		w.WriteHeader(http.StatusUnauthorized)
		render(w, r, templates.LoginWithNext("Invalid email or password.", email, next))
		return
	}

	_ = h.throttle.Record(r.Context(), email, clientIP, true)

	// MFA gate: the password was correct, but a session is not authenticated
	// until the TOTP step also passes. Hold it pending and divert to /login/mfa.
	if mfaEnabled {
		if err := h.sessions.CreatePending(r.Context(), w, id); err != nil {
			http.Error(w, "session error", http.StatusInternalServerError)
			return
		}
		dest := "/login/mfa"
		if next != "" {
			dest += "?next=" + url.QueryEscape(next)
		}
		http.Redirect(w, r, dest, http.StatusSeeOther)
		return
	}

	// Pick an account to land them on: prefer their personal account if it
	// still exists; otherwise any account they're a member of.
	currentAccount := h.pickDefaultAccount(r.Context(), id)

	if err := h.sessions.Create(r.Context(), w, id, currentAccount); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: nilIfZero(currentAccount),
		ActorID:   &id,
		Action:    "login.succeeded",
		IP:        clientIP,
		UserAgent: r.UserAgent(),
	})
	http.Redirect(w, r, redirectTarget(next), http.StatusSeeOther)
}

func (h *Handlers) signupPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.FromContext(r.Context()); ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	// Self-serve signup is feature-flagged: off → sales-led GA.
	if !h.flags.Enabled(r.Context(), flags.SelfServeSignup, clientIPKey(r)) {
		render(w, r, templates.SignupClosed())
		return
	}
	render(w, r, templates.SignupWithNext("", "", safeNext(r.URL.Query().Get("next"))))
}

func (h *Handlers) signupSubmit(w http.ResponseWriter, r *http.Request) {
	if !h.flags.Enabled(r.Context(), flags.SelfServeSignup, clientIPKey(r)) {
		w.WriteHeader(http.StatusForbidden)
		render(w, r, templates.SignupClosed())
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(strings.ToLower(r.PostFormValue("email")))
	password := r.PostFormValue("password")
	next := safeNext(r.PostFormValue("next"))

	if email == "" || !strings.Contains(email, "@") {
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, templates.SignupWithNext("Enter a valid email.", email, next))
		return
	}
	if len(password) < 12 {
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, templates.SignupWithNext("Password must be at least 12 characters.", email, next))
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		http.Error(w, "hash error", http.StatusInternalServerError)
		return
	}

	id, err := insertUser(r.Context(), h, email, hash)
	if err != nil {
		if errors.Is(err, errEmailTaken) {
			w.WriteHeader(http.StatusConflict)
			render(w, r, templates.SignupWithNext("That email is already registered.", email, next))
			return
		}
		http.Error(w, "create user", http.StatusInternalServerError)
		return
	}

	// Promote staff from the STAFF_EMAILS allowlist (interim until SSO).
	if h.staffEmails[email] {
		if _, err := h.pool.Exec(r.Context(),
			`UPDATE users SET is_staff = TRUE WHERE id = $1`, id); err != nil {
			h.logger().Warn("staff promotion failed", "user_id", id, "err", err)
		}
	}

	// Auto-create the personal account + owner membership.
	acct, err := h.accounts.CreatePersonalAccount(r.Context(), id, email)
	if err != nil {
		http.Error(w, "create account: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &acct.ID, ActorID: &id, Action: "account.created",
		TargetKind: "account", TargetID: acct.ID.String(),
		Metadata: map[string]any{"name": acct.Name, "kind": "personal"},
	})

	// Best-effort Stripe customer creation. Failures here are logged but
	// don't block signup; the account works fine without billing.
	if h.billing.Enabled() {
		if customerID, err := h.billing.Create(r.Context(), acct.ID, email, acct.Name); err == nil && customerID != "" {
			_ = h.accounts.SetStripeCustomerID(r.Context(), acct.ID, customerID)
		}
	}

	if err := h.sessions.Create(r.Context(), w, id, acct.ID); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &acct.ID, ActorID: &id, Action: "user.signed_up",
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
	})

	// Growth: capture the funnel event and send the welcome email. Both are
	// best-effort — neither blocks the redirect into the app.
	h.analytics.Capture(id.String(), analytics.EventSignedUp, map[string]any{
		"account_id": acct.ID.String(),
	})
	if err := h.mailer.Send(r.Context(), mail.WelcomeMessage(email, acct.Name)); err != nil &&
		!errors.Is(err, mail.ErrSuppressed) {
		h.logger().Warn("welcome email failed", "to", email, "err", err)
	}

	// Send the email-verification link. Best-effort: a failure here doesn't
	// block signup — the user can resend from the in-app banner.
	if token, err := h.sessions.CreateVerificationToken(r.Context(), id); err != nil {
		h.logger().Warn("verification token failed", "user_id", id, "err", err)
	} else {
		link := absoluteURL(r, "/verify-email/"+token)
		if err := h.mailer.Send(r.Context(), mail.VerifyEmailMessage(email, link)); err != nil &&
			!errors.Is(err, mail.ErrSuppressed) {
			h.logger().Warn("verification email failed", "to", email, "err", err)
		}
	}

	http.Redirect(w, r, redirectTarget(next), http.StatusSeeOther)
}

// safeNext sanitizes a post-auth redirect target. Only same-origin absolute
// paths are allowed (must start with a single "/"), defeating open-redirect.
func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return ""
	}
	return next
}

func redirectTarget(next string) string {
	if next == "" {
		return "/dashboard"
	}
	return next
}

func (h *Handlers) logout(w http.ResponseWriter, r *http.Request) {
	if u, ok := auth.FromContext(r.Context()); ok {
		var acct *uuid.UUID
		if a, ok := auth.CurrentAccountFromContext(r.Context()); ok {
			acct = &a.ID
		}
		_ = h.audit.Record(r.Context(), audit.Event{
			AccountID: acct,
			ActorID:   &u.ID,
			Action:    "logout",
			IP:        audit.ClientIP(r),
			UserAgent: r.UserAgent(),
		})
	}
	_ = h.sessions.Destroy(r.Context(), w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handlers) dashboard(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	dv := templates.DashboardView{Ctx: lc}
	if lc.Account != nil {
		dv.Targets, _ = h.drills.ListTargets(r.Context(), lc.Account.ID)
		dv.Drills, _ = h.drills.ListDrills(r.Context(), lc.Account.ID, 10)
	}
	render(w, r, templates.Dashboard(dv))
}

// pickDefaultAccount returns the account the user should land on after
// login. We pick the oldest account they own; otherwise the oldest one
// they're a member of; uuid.Nil if none.
func (h *Handlers) pickDefaultAccount(ctx context.Context, userID uuid.UUID) uuid.UUID {
	accounts, err := h.accounts.ListAccountsForUser(ctx, userID)
	if err != nil || len(accounts) == 0 {
		return uuid.Nil
	}
	for _, a := range accounts {
		if a.Role == account.RoleOwner {
			return a.ID
		}
	}
	return accounts[0].ID
}

func nilIfZero(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

var errEmailTaken = errors.New("email already registered")

func insertUser(ctx context.Context, h *Handlers, email, hash string) (uuid.UUID, error) {
	var id uuid.UUID
	err := h.pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash) VALUES ($1, $2)
		RETURNING id
	`, email, hash).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return uuid.Nil, errEmailTaken
		}
		return uuid.Nil, err
	}
	return id, nil
}

func isUniqueViolation(err error) bool {
	// pgx wraps the pg error; SQLSTATE 23505 is unique_violation.
	type sqlState interface{ SQLState() string }
	var s sqlState
	if errors.As(err, &s) {
		return s.SQLState() == "23505"
	}
	// Fallback string sniff to stay resilient across driver versions.
	return strings.Contains(err.Error(), "23505") ||
		strings.Contains(strings.ToLower(err.Error()), "duplicate key")
}

// dummyHash mirrors a real Argon2id verify when the user lookup fails, so the
// response time can't be used to enumerate registered emails.
const dummyHash = "$argon2id$v=19$m=19456,t=2,p=1$" +
	"AAAAAAAAAAAAAAAAAAAAAA$" +
	"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
