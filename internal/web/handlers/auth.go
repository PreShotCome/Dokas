package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/auth"
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
	render(w, r, templates.Login("", ""))
}

func (h *Handlers) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(strings.ToLower(r.PostFormValue("email")))
	password := r.PostFormValue("password")

	if email == "" || password == "" {
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, templates.Login("Enter an email and password.", email))
		return
	}

	var id uuid.UUID
	var hash string
	err := h.pool.QueryRow(r.Context(), `
		SELECT id, password_hash FROM users
		 WHERE email = $1 AND deleted_at IS NULL
	`, email).Scan(&id, &hash)
	if err != nil {
		// Constant-ish time: always run a verify against a dummy hash.
		_ = auth.VerifyPassword(password, dummyHash)
		w.WriteHeader(http.StatusUnauthorized)
		render(w, r, templates.Login("Invalid email or password.", email))
		return
	}

	if err := auth.VerifyPassword(password, hash); err != nil {
		_ = h.audit.Record(r.Context(), audit.Event{
			Action:    "login.failed",
			TargetID:  id.String(),
			IP:        audit.ClientIP(r),
			UserAgent: r.UserAgent(),
		})
		w.WriteHeader(http.StatusUnauthorized)
		render(w, r, templates.Login("Invalid email or password.", email))
		return
	}

	if err := h.sessions.Create(r.Context(), w, id); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		ActorID:   &id,
		Action:    "login.succeeded",
		IP:        audit.ClientIP(r),
		UserAgent: r.UserAgent(),
	})
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *Handlers) signupPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.FromContext(r.Context()); ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	render(w, r, templates.Signup("", ""))
}

func (h *Handlers) signupSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(strings.ToLower(r.PostFormValue("email")))
	password := r.PostFormValue("password")

	if email == "" || !strings.Contains(email, "@") {
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, templates.Signup("Enter a valid email.", email))
		return
	}
	if len(password) < 12 {
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, templates.Signup("Password must be at least 12 characters.", email))
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
			render(w, r, templates.Signup("That email is already registered.", email))
			return
		}
		http.Error(w, "create user", http.StatusInternalServerError)
		return
	}

	if err := h.sessions.Create(r.Context(), w, id); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		ActorID:   &id,
		Action:    "user.signed_up",
		IP:        audit.ClientIP(r),
		UserAgent: r.UserAgent(),
	})
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *Handlers) logout(w http.ResponseWriter, r *http.Request) {
	if u, ok := auth.FromContext(r.Context()); ok {
		_ = h.audit.Record(r.Context(), audit.Event{
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
	u, _ := auth.FromContext(r.Context())
	targets, _ := h.drills.ListTargets(r.Context(), u.ID)
	recent, _ := h.drills.ListDrills(r.Context(), u.ID, 10)
	render(w, r, templates.Dashboard(u, targets, recent))
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
