package auth

import (
	"context"
	"net/http"
)

type ctxKey int

const (
	userCtxKey ctxKey = iota
)

// FromContext returns the authenticated user, if any.
func FromContext(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(userCtxKey).(*User)
	return u, ok
}

// WithUser stamps the user onto the context.
func WithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, userCtxKey, u)
}

// LoadUser is a middleware that resolves the session cookie (if any) and
// stamps the user onto the request context. It does NOT require auth.
func (s *Store) LoadUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _, err := s.Lookup(r.Context(), r)
		if err == nil {
			r = r.WithContext(WithUser(r.Context(), u))
		}
		next.ServeHTTP(w, r)
	})
}

// RequireUser is a middleware that 302s to /login when no session is present.
func RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := FromContext(r.Context()); !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
