// Package csrf implements double-submit-cookie CSRF protection.
//
// The middleware ensures a random token cookie exists on every request and
// stamps the token onto the request context. On unsafe verbs (POST/PUT/
// PATCH/DELETE) it requires a matching token in the `_csrf` form field.
//
// The session cookie is already SameSite=Lax, which blocks most cross-site
// form posts on its own; this is the defense-in-depth second layer the plan
// calls for.
package csrf

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
)

// FieldName is the form field (and the templ helper) the token travels in.
const FieldName = "_csrf"

const (
	cookieNameSecure   = "__Host-rd_csrf"
	cookieNameInsecure = "rd_csrf"
)

type ctxKey int

const tokenCtxKey ctxKey = 0

// Token returns the CSRF token stamped on the context by Middleware. Templ
// templates call this with the `ctx` that templ injects, so every form can
// render a hidden field without the handler threading the token through.
func Token(ctx context.Context) string {
	t, _ := ctx.Value(tokenCtxKey).(string)
	return t
}

// Protector issues and verifies CSRF tokens. secure selects the cookie name
// + Secure attribute (true in production, false in dev so cookies work over
// plain HTTP).
type Protector struct {
	secure bool
	// exempt path prefixes — inbound webhook receivers authenticate with
	// their own token, not a CSRF cookie, so they bypass the check.
	exempt []string
}

// New builds a Protector. exemptPrefixes lists path prefixes (e.g.
// "/webhooks/") whose unsafe requests skip CSRF validation.
func New(secure bool, exemptPrefixes ...string) *Protector {
	return &Protector{secure: secure, exempt: exemptPrefixes}
}

func (p *Protector) isExempt(path string) bool {
	for _, prefix := range p.exempt {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func (p *Protector) cookieName() string {
	if p.secure {
		return cookieNameSecure
	}
	return cookieNameInsecure
}

// Middleware ensures a token cookie exists, stamps the token on the context,
// and rejects unsafe requests whose `_csrf` field doesn't match the cookie.
func (p *Protector) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := p.readCookie(r)
		if token == "" {
			token = newToken()
			p.setCookie(w, token)
		}
		ctx := context.WithValue(r.Context(), tokenCtxKey, token)
		r = r.WithContext(ctx)

		if isUnsafe(r.Method) && !p.isExempt(r.URL.Path) {
			submitted := r.PostFormValue(FieldName)
			if submitted == "" || subtle.ConstantTimeCompare([]byte(submitted), []byte(token)) != 1 {
				http.Error(w, "CSRF token invalid or missing", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (p *Protector) readCookie(r *http.Request) string {
	c, err := r.Cookie(p.cookieName())
	if err != nil {
		return ""
	}
	return c.Value
}

func (p *Protector) setCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     p.cookieName(),
		Value:    token,
		Path:     "/",
		Secure:   p.secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func isUnsafe(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func newToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// rand.Read failing means the platform RNG is broken; fail loud
		// rather than issue a predictable token.
		panic("csrf: cannot read random bytes: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
