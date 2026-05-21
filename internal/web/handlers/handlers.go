package handlers

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preshotcome/anything/internal/account"
	"github.com/preshotcome/anything/internal/analytics"
	"github.com/preshotcome/anything/internal/apikey"
	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/auth"
	"github.com/preshotcome/anything/internal/billing"
	"github.com/preshotcome/anything/internal/compliance"
	"github.com/preshotcome/anything/internal/drill"
	"github.com/preshotcome/anything/internal/email"
	"github.com/preshotcome/anything/internal/evidence"
	"github.com/preshotcome/anything/internal/flags"
	"github.com/preshotcome/anything/internal/obs"
	"github.com/preshotcome/anything/internal/ratelimit"
	"github.com/preshotcome/anything/internal/web/csrf"
	"github.com/preshotcome/anything/internal/web/templates"
	"github.com/preshotcome/anything/internal/webhooks"
)

type Handlers struct {
	pool            *pgxpool.Pool
	sessions        *auth.Store
	audit           *audit.Logger
	drills          *drill.Store
	orch            *drill.Orchestrator
	accounts        *account.Store
	billing         billing.Customers
	throttle        *auth.LoginThrottle
	webhooks        *webhooks.Store
	webhookDispatch *webhooks.Dispatcher
	csrf            *csrf.Protector
	authLimiter     *ratelimit.Limiter
	appLimiter      *ratelimit.Limiter
	evidence        *evidence.Service
	exporter        *compliance.Exporter
	purger          *compliance.Purger
	inserter        drill.RiverInserter
	obs             *obs.Provider

	mailer               *email.Mailer
	analytics            analytics.Analytics
	flags                flags.Flags
	postmarkWebhookToken string
	staffEmails          map[string]bool
	metricsToken         string
	apiKeys              *apikey.Store
	v1Limiter            *ratelimit.Limiter
}

type Deps struct {
	Pool            *pgxpool.Pool
	Sessions        *auth.Store
	Audit           *audit.Logger
	Drills          *drill.Store
	Orchestrator    *drill.Orchestrator
	Accounts        *account.Store
	Billing         billing.Customers
	Throttle        *auth.LoginThrottle
	Webhooks        *webhooks.Store
	WebhookDispatch *webhooks.Dispatcher
	CSRF            *csrf.Protector
	AuthLimiter     *ratelimit.Limiter
	AppLimiter      *ratelimit.Limiter
	Evidence        *evidence.Service
	Exporter        *compliance.Exporter
	Purger          *compliance.Purger
	Inserter        drill.RiverInserter
	Obs             *obs.Provider

	Mailer               *email.Mailer
	Analytics            analytics.Analytics
	Flags                flags.Flags
	PostmarkWebhookToken string
	StaffEmails          []string
	MetricsToken         string
	APIKeys              *apikey.Store
	V1Limiter            *ratelimit.Limiter
}

func New(d Deps) *Handlers {
	staff := make(map[string]bool, len(d.StaffEmails))
	for _, e := range d.StaffEmails {
		staff[e] = true
	}
	return &Handlers{
		pool:            d.Pool,
		sessions:        d.Sessions,
		audit:           d.Audit,
		drills:          d.Drills,
		orch:            d.Orchestrator,
		accounts:        d.Accounts,
		billing:         d.Billing,
		throttle:        d.Throttle,
		webhooks:        d.Webhooks,
		webhookDispatch: d.WebhookDispatch,
		csrf:            d.CSRF,
		authLimiter:     d.AuthLimiter,
		appLimiter:      d.AppLimiter,
		evidence:        d.Evidence,
		exporter:        d.Exporter,
		purger:          d.Purger,
		inserter:        d.Inserter,
		obs:             d.Obs,

		mailer:               d.Mailer,
		analytics:            d.Analytics,
		flags:                d.Flags,
		postmarkWebhookToken: d.PostmarkWebhookToken,
		staffEmails:          staff,
		metricsToken:         d.MetricsToken,
		apiKeys:              d.APIKeys,
		v1Limiter:            d.V1Limiter,
	}
}

// logger returns the package-default slog logger. Wrapped in a method so
// future per-request loggers can replace it without touching every handler.
func (h *Handlers) logger() *slog.Logger { return slog.Default() }

// layoutCtx assembles the nav chrome state for a request: the user, their
// current account + role, and the full account list for the switcher.
func (h *Handlers) layoutCtx(r *http.Request) templates.LayoutCtx {
	u, _ := auth.FromContext(r.Context())
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	m, _ := auth.MembershipFromContext(r.Context())
	lc := templates.LayoutCtx{User: u, Account: acct, Membership: m}
	if imp, ok := auth.ImpersonationFromContext(r.Context()); ok {
		lc.Impersonation = imp
	}
	if u != nil {
		accts, _ := h.accounts.ListAccountsForUser(r.Context(), u.ID)
		for _, a := range accts {
			lc.Accounts = append(lc.Accounts, templates.AccountChoice{
				ID: a.ID.String(), Name: a.Name,
			})
		}
	}
	return lc
}

func (h *Handlers) Router(staticFS http.FileSystem) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(obs.TracingMiddleware)         // open the server span first
	r.Use(obs.RequestLogger(h.logger())) // one structured line per request
	r.Use(h.obs.Metrics.Middleware)      // request count + latency
	r.Use(h.obs.Recoverer)               // panic → error reporter → 500
	r.Use(securityHeaders)
	r.Use(h.sessions.LoadUser)
	r.Use(h.sessions.LoadCurrentAccount(h.accounts))
	r.Use(stampAccountForLogs) // enrich the request log with account_id
	r.Use(h.csrf.Middleware)

	// Liveness: the process is up. Readiness: dependencies are reachable.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Get("/readyz", obs.ReadinessHandler(map[string]func(context.Context) error{
		"database": h.pool.Ping,
	}))
	r.Handle("/metrics", h.metricsHandler())
	r.Get("/robots.txt", h.robotsTxt)

	// Inbound Postmark bounce/complaint webhook — authenticated by the
	// token path segment, CSRF-exempt (see csrf.New in main).
	r.Post("/webhooks/postmark/{token}", h.postmarkBounce)

	// The versioned JSON API: API-key auth, no session/CSRF (csrf.New
	// exempts the /v1/ prefix). Mounted at the top level so it's outside
	// the session-gated group.
	r.Mount("/v1", h.v1Router())
	// API docs are public.
	r.Get("/openapi.json", h.openAPISpec)
	r.Get("/docs", h.docsPage)

	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(staticFS)))

	r.Get("/", h.index)

	// Auth endpoints get a tight per-IP rate limit on top of the login
	// throttle: the throttle protects one account, the limiter protects the
	// whole box from a credential-stuffing flood.
	r.Group(func(r chi.Router) {
		r.Use(h.authLimiter.Middleware(clientIPKey))
		r.Get("/login", h.loginPage)
		r.Post("/login", h.loginSubmit)
		r.Get("/signup", h.signupPage)
		r.Post("/signup", h.signupSubmit)
		// Second login step — driven by the mfa_pending session cookie, so
		// it sits with the other pre-auth routes under the per-IP limiter.
		r.Get("/login/mfa", h.mfaChallengePage)
		r.Post("/login/mfa", h.mfaChallengeSubmit)
	})
	r.Post("/logout", h.logout)

	// Invitation accept page — public so the recipient can land before
	// signing up; POST requires login.
	r.Get("/invitations/{token}", h.invitationPage)
	r.Post("/invitations/{token}/accept", h.invitationAccept)

	// Email verification: the link from the signup email is public so it
	// works before the recipient signs in.
	r.Get("/verify-email/{token}", h.verifyEmail)

	// Legal + help pages are public.
	r.Get("/legal/terms", h.legalTerms)
	r.Get("/legal/privacy", h.legalPrivacy)
	r.Get("/legal/dpa", h.legalDPA)
	r.Get("/legal/subprocessors", h.legalSubprocessors)
	r.Get("/legal/cookies", h.legalCookies)
	r.Get("/help", h.helpPage)

	// Ending an impersonation: requires login but NOT staff — mid-
	// impersonation the effective user is the (non-staff) target.
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireUser)
		r.Post("/impersonate/stop", h.impersonateStop)
	})

	// Staff admin panel — staff-gated, no account requirement (staff act
	// across accounts).
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireUser)
		r.Use(auth.RequireStaff)
		r.Get("/admin", h.adminHome)
		r.Get("/admin/users", h.adminUserSearch)
		r.Get("/admin/users/{id}", h.adminUserDetail)
		r.Post("/admin/users/{id}/impersonate", h.adminImpersonate)
		r.Get("/admin/drills/{id}", h.adminDrillDetail)
		r.Post("/admin/drills/{id}/replay", h.adminDrillReplay)
		r.Post("/admin/drills/{id}/regen-evidence", h.adminEvidenceRegen)
	})

	r.Group(func(r chi.Router) {
		r.Use(auth.RequireUser)
		r.Use(auth.RequireAccount)
		// Authenticated traffic is rate-limited per account.
		r.Use(h.appLimiter.Middleware(accountKey))

		r.Get("/dashboard", h.dashboard)
		r.Post("/account/switch", h.accountSwitch)
		r.Post("/verify-email/resend", h.verifyEmailResend)

		// Two-factor auth — a per-user security setting, not RBAC-gated.
		r.Get("/account/mfa", h.mfaSetupPage)
		r.Post("/account/mfa/enable", h.mfaEnable)
		r.Post("/account/mfa/disable", h.mfaDisable)

		// Reads
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAction(auth.ActionTargetRead))
			r.Get("/databases", h.targetsList)
			r.Get("/databases/{id}", h.targetDetail)
		})
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAction(auth.ActionDrillRead))
			r.Get("/drills", h.drillsList)
			r.Get("/drills/{id}", h.drillDetail)
			r.Get("/drills/{id}/steps", h.drillStepsPartial)
		})
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAction(auth.ActionEvidenceRead))
			r.Get("/drills/{id}/evidence", h.drillEvidence)
		})
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAction(auth.ActionAccountRead))
			r.Get("/account", h.accountSettings)
			r.Get("/account/webhooks", h.webhooksList)
			r.Get("/account/webhooks/{id}/deliveries", h.webhookDeliveries)
			r.Get("/account/api-keys", h.apiKeysList)
		})

		// Writes (RBAC-gated)
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAction(auth.ActionTargetWrite))
			r.Get("/databases/new", h.targetNewPage)
			r.Post("/databases", h.targetCreate)
			r.Post("/databases/{id}/assertions", h.assertionCreate)
			r.Post("/databases/{id}/assertions/{assertion_id}/delete", h.assertionDelete)
		})
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAction(auth.ActionDrillWrite))
			r.Post("/drills", h.drillCreate)
		})
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAction(auth.ActionMemberWrite))
			r.Post("/account/invitations", h.inviteCreate)
			r.Post("/account/members/{user_id}", h.memberUpdate)
			r.Post("/account/members/{user_id}/remove", h.memberRemove)
			r.Post("/account/members/{user_id}/transfer-ownership", h.memberTransferOwnership)
		})
		// Webhook management + compliance actions are account-write
		// concerns. The delete handler additionally requires owner.
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAction(auth.ActionAccountWrite))
			r.Post("/account/webhooks", h.webhookCreate)
			r.Post("/account/webhooks/{id}/delete", h.webhookDelete)
			r.Post("/account/webhooks/{id}/deliveries/{delivery_id}/replay", h.webhookReplay)
			r.Post("/account/api-keys", h.apiKeyCreate)
			r.Post("/account/api-keys/{id}/revoke", h.apiKeyRevoke)
			r.Get("/account/export", h.accountExport)
			r.Post("/account/delete", h.accountDelete)
		})
	})

	return r
}

// metricsHandler serves Prometheus metrics. When METRICS_TOKEN is set it
// requires a matching bearer token; unset leaves /metrics open for local
// dev. Production should set the token (or scrape over a private network).
func (h *Handlers) metricsHandler() http.Handler {
	inner := h.obs.Metrics.Handler()
	if h.metricsToken == "" {
		return inner
	}
	want := "Bearer " + h.metricsToken
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(want)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		inner.ServeHTTP(w, r)
	})
}

// stampAccountForLogs enriches the request-scoped log fields with the
// resolved account ID, once LoadCurrentAccount has run.
func stampAccountForLogs(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a, ok := auth.CurrentAccountFromContext(r.Context()); ok {
			r = r.WithContext(obs.WithAccountID(r.Context(), a.ID.String()))
		}
		next.ServeHTTP(w, r)
	})
}

// clientIPKey buckets the rate limiter by client IP.
func clientIPKey(r *http.Request) string {
	return "ip:" + audit.ClientIP(r)
}

// accountKey buckets by current account, falling back to IP when somehow
// account context is missing (shouldn't happen behind RequireAccount).
func accountKey(r *http.Request) string {
	if a, ok := auth.CurrentAccountFromContext(r.Context()); ok {
		return "acct:" + a.ID.String()
	}
	return "ip:" + audit.ClientIP(r)
}

// securityHeaders sets a baseline set of headers. CSP is intentionally strict;
// inline scripts are not allowed. Tighten further per route as needed.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self'; "+
				"style-src 'self'; "+
				"img-src 'self' data:; "+
				"font-src 'self'; "+
				"connect-src 'self'; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self'")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		// HSTS is set unconditionally; harmless on HTTP, mandatory on HTTPS.
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		next.ServeHTTP(w, r)
	})
}
