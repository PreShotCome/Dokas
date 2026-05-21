package auth

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/preshotcome/anything/internal/account"
)

type ctxKey int

const (
	userCtxKey ctxKey = iota
	accountCtxKey
	membershipCtxKey
	impersonationCtxKey
)

// Impersonation describes an active staff impersonation: the effective user
// (from FromContext) is being acted-as by this staff member.
type Impersonation struct {
	StaffUserID uuid.UUID
	StaffEmail  string
}

// ImpersonationFromContext returns the active impersonation, if any.
func ImpersonationFromContext(ctx context.Context) (*Impersonation, bool) {
	imp, ok := ctx.Value(impersonationCtxKey).(*Impersonation)
	return imp, ok
}

func withImpersonation(ctx context.Context, imp *Impersonation) context.Context {
	return context.WithValue(ctx, impersonationCtxKey, imp)
}

// FromContext returns the authenticated user, if any.
func FromContext(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(userCtxKey).(*User)
	return u, ok
}

// WithUser stamps the user onto the context.
func WithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, userCtxKey, u)
}

// CurrentAccountFromContext returns the account the request is acting on.
func CurrentAccountFromContext(ctx context.Context) (*account.Account, bool) {
	a, ok := ctx.Value(accountCtxKey).(*account.Account)
	return a, ok
}

// WithCurrentAccount stamps the current account onto the context.
func WithCurrentAccount(ctx context.Context, a *account.Account) context.Context {
	return context.WithValue(ctx, accountCtxKey, a)
}

// MembershipFromContext returns the (user, account) membership, used by the
// Authorize check.
func MembershipFromContext(ctx context.Context) (*account.Membership, bool) {
	m, ok := ctx.Value(membershipCtxKey).(*account.Membership)
	return m, ok
}

func WithMembership(ctx context.Context, m *account.Membership) context.Context {
	return context.WithValue(ctx, membershipCtxKey, m)
}

// LoadUser is a middleware that resolves the session cookie (if any) and
// stamps the effective user onto the request context. When the session is
// an impersonation, it also stamps the staff impersonator. Does NOT require
// auth.
func (s *Store) LoadUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, sess, err := s.Lookup(r.Context(), r)
		// An mfa_pending session has a correct password but no MFA code yet;
		// it must not authenticate app requests, so leave the user unset.
		if err == nil && !sess.MFAPending {
			ctx := WithUser(r.Context(), u)
			if sess.ImpersonatorID != nil {
				if staff, err := s.loadUser(ctx, *sess.ImpersonatorID); err == nil {
					ctx = withImpersonation(ctx, &Impersonation{
						StaffUserID: staff.ID, StaffEmail: staff.Email,
					})
				}
			}
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// RequireStaff is a middleware that 404s non-staff users (a 404, not a 403,
// so the admin surface isn't even acknowledged to non-staff). It must run
// after RequireUser.
func RequireStaff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := FromContext(r.Context())
		if !ok || !u.IsStaff {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// LoadCurrentAccount is a middleware that resolves the session's
// current_account_id to a full Account + Membership and stamps both on the
// context. If the user is logged in but has no current_account_id set (or
// the row was deleted), the middleware lazily picks any account the user
// belongs to. If the user belongs to none, the middleware passes through
// without account context (handlers gated by RequireRole will 403).
type AccountResolver interface {
	GetAccount(ctx context.Context, id uuid.UUID) (account.Account, error)
	GetMembership(ctx context.Context, accountID, userID uuid.UUID) (account.Membership, error)
	ListAccountsForUser(ctx context.Context, userID uuid.UUID) ([]struct {
		account.Account
		Role account.Role
	}, error)
}

// LoadCurrentAccount needs both the session store (to read the cookie's
// current_account_id) and an account resolver.
//
// Resolution order:
//  1. Use the session's persisted current_account_id if the user is still a
//     member of it.
//  2. Otherwise fall back to the first account the user belongs to (and
//     persist that choice so it sticks). This covers a removed-from-account
//     race: the user was kicked but their session still pointed there.
//  3. If the user belongs to no account, pass through without account
//     context; RequireAccount will 403.
func (s *Store) LoadCurrentAccount(resolver AccountResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := FromContext(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			ctx := r.Context()

			currentID, _ := s.CurrentAccountID(ctx, r)

			// Verify membership on the persisted account; clear it if gone.
			if currentID != uuid.Nil {
				if _, err := resolver.GetMembership(ctx, currentID, u.ID); err != nil {
					currentID = uuid.Nil
				}
			}

			// Fall back to any account the user belongs to.
			if currentID == uuid.Nil {
				accounts, _ := resolver.ListAccountsForUser(ctx, u.ID)
				if len(accounts) > 0 {
					currentID = accounts[0].ID
					_ = s.SetCurrentAccount(ctx, r, currentID)
				}
			}

			if currentID != uuid.Nil {
				acct, accErr := resolver.GetAccount(ctx, currentID)
				m, memErr := resolver.GetMembership(ctx, currentID, u.ID)
				if accErr == nil && memErr == nil {
					ctx = WithCurrentAccount(ctx, &acct)
					ctx = WithMembership(ctx, &m)
				}
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
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

// RequireAccount is a middleware that 302s to /account when the user is
// logged in but no current account is set. In practice this only happens
// for users created before Phase 3; signup now always creates one.
func RequireAccount(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := CurrentAccountFromContext(r.Context()); !ok {
			http.Error(w, "no account on session", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
