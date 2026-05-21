package csrf

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSafeRequestGetsTokenCookie(t *testing.T) {
	p := New(false)
	var stampedToken string
	h := p.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		stampedToken = Token(r.Context())
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))

	if stampedToken == "" {
		t.Fatal("token not stamped on context for GET")
	}
	if !strings.Contains(rec.Header().Get("Set-Cookie"), cookieNameInsecure) {
		t.Fatalf("expected csrf cookie to be set, got %q", rec.Header().Get("Set-Cookie"))
	}
}

func TestUnsafeRequestRejectedWithoutToken(t *testing.T) {
	p := New(false)
	h := p.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/drills", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: cookieNameInsecure, Value: "sometoken"})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST without _csrf field: got %d, want 403", rec.Code)
	}
}

func TestUnsafeRequestRejectedWithMismatchedToken(t *testing.T) {
	p := New(false)
	h := p.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	form := url.Values{FieldName: {"wrong"}}
	req := httptest.NewRequest(http.MethodPost, "/drills", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: cookieNameInsecure, Value: "correct"})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST with mismatched token: got %d, want 403", rec.Code)
	}
}

func TestUnsafeRequestAcceptedWithMatchingToken(t *testing.T) {
	p := New(false)
	var bodyServed bool
	h := p.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		bodyServed = true
		w.WriteHeader(http.StatusOK)
	}))

	const token = "matching-token-value"
	form := url.Values{FieldName: {token}}
	req := httptest.NewRequest(http.MethodPost, "/drills", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: cookieNameInsecure, Value: token})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bodyServed {
		t.Fatalf("POST with matching token: got %d (served=%v), want 200", rec.Code, bodyServed)
	}
}

// The token issued on a GET must round-trip: read the cookie, submit it,
// and a POST is accepted.
func TestTokenRoundTrip(t *testing.T) {
	p := New(false)
	h := p.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	getRec := httptest.NewRecorder()
	h.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/login", nil))
	cookie := getRec.Result().Cookies()[0]

	form := url.Values{FieldName: {cookie.Value}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)

	postRec := httptest.NewRecorder()
	h.ServeHTTP(postRec, req)
	if postRec.Code != http.StatusOK {
		t.Fatalf("round-trip POST: got %d, want 200", postRec.Code)
	}
	_ = io.Discard
}
