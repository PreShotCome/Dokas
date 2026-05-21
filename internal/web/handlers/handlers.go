package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/auth"
	"github.com/preshotcome/anything/internal/drill"
)

type Handlers struct {
	pool     *pgxpool.Pool
	sessions *auth.Store
	audit    *audit.Logger
	drills   *drill.Store
	orch     *drill.Orchestrator
}

type Deps struct {
	Pool         *pgxpool.Pool
	Sessions     *auth.Store
	Audit        *audit.Logger
	Drills       *drill.Store
	Orchestrator *drill.Orchestrator
}

func New(d Deps) *Handlers {
	return &Handlers{
		pool:     d.Pool,
		sessions: d.Sessions,
		audit:    d.Audit,
		drills:   d.Drills,
		orch:     d.Orchestrator,
	}
}

func (h *Handlers) Router(staticFS http.FileSystem) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)
	r.Use(h.sessions.LoadUser)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(staticFS)))

	r.Get("/", h.index)
	r.Get("/login", h.loginPage)
	r.Post("/login", h.loginSubmit)
	r.Get("/signup", h.signupPage)
	r.Post("/signup", h.signupSubmit)
	r.Post("/logout", h.logout)

	r.Group(func(r chi.Router) {
		r.Use(auth.RequireUser)
		r.Get("/dashboard", h.dashboard)

		r.Get("/databases", h.targetsList)
		r.Get("/databases/new", h.targetNewPage)
		r.Post("/databases", h.targetCreate)

		r.Get("/drills", h.drillsList)
		r.Post("/drills", h.drillCreate)
		r.Get("/drills/{id}", h.drillDetail)
		r.Get("/drills/{id}/steps", h.drillStepsPartial)
		r.Get("/drills/{id}/evidence", h.drillEvidence)
	})

	return r
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
