package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRequireStaff(t *testing.T) {
	staffNext := false
	h := RequireStaff(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		staffNext = true
		w.WriteHeader(http.StatusOK)
	}))

	// A staff user passes.
	staffNext = false
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req = req.WithContext(WithUser(req.Context(), &User{ID: uuid.New(), IsStaff: true}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !staffNext {
		t.Fatalf("staff user: code=%d reached=%v, want 200/true", rec.Code, staffNext)
	}

	// A non-staff user gets 404 (admin surface not acknowledged).
	staffNext = false
	req = httptest.NewRequest(http.MethodGet, "/admin", nil)
	req = req.WithContext(WithUser(req.Context(), &User{ID: uuid.New(), IsStaff: false}))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound || staffNext {
		t.Fatalf("non-staff user: code=%d reached=%v, want 404/false", rec.Code, staffNext)
	}

	// No user at all → 404.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("anonymous: code=%d, want 404", rec.Code)
	}
}

func TestImpersonationLifecycle(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	staffID := seedAuthUser(t, ctx, pool, true)
	targetID := seedAuthUser(t, ctx, pool, false)

	store := NewStore(pool, 14*24*time.Hour, 30*24*time.Hour, false)

	// Create a session for the staff user and capture the cookie.
	rec := httptest.NewRecorder()
	if err := store.Create(ctx, rec, staffID, uuid.Nil); err != nil {
		t.Fatalf("create session: %v", err)
	}
	cookie := rec.Result().Cookies()[0]
	withCookie := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(cookie)
		return r
	}

	// Baseline: the session resolves to the staff user, not impersonating.
	u, sess, err := store.Lookup(ctx, withCookie())
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if u.ID != staffID || !u.IsStaff {
		t.Fatalf("baseline user = %s staff=%v, want staff %s", u.ID, u.IsStaff, staffID)
	}
	if sess.ImpersonatorID != nil {
		t.Fatal("fresh session should not be impersonating")
	}

	// Start impersonation → the session now resolves to the target, with
	// the staff user recorded as impersonator.
	if err := store.StartImpersonation(ctx, withCookie(), staffID, targetID); err != nil {
		t.Fatalf("start impersonation: %v", err)
	}
	u, sess, err = store.Lookup(ctx, withCookie())
	if err != nil {
		t.Fatalf("lookup while impersonating: %v", err)
	}
	if u.ID != targetID {
		t.Fatalf("impersonated user = %s, want target %s", u.ID, targetID)
	}
	if sess.ImpersonatorID == nil || *sess.ImpersonatorID != staffID {
		t.Fatalf("ImpersonatorID = %v, want %s", sess.ImpersonatorID, staffID)
	}

	// Starting again is rejected — already impersonating.
	if err := store.StartImpersonation(ctx, withCookie(), staffID, targetID); err == nil {
		t.Fatal("a second StartImpersonation should fail")
	}

	// Stop → back to the staff user, impersonator cleared.
	if err := store.StopImpersonation(ctx, withCookie()); err != nil {
		t.Fatalf("stop impersonation: %v", err)
	}
	u, sess, err = store.Lookup(ctx, withCookie())
	if err != nil {
		t.Fatalf("lookup after stop: %v", err)
	}
	if u.ID != staffID {
		t.Fatalf("after stop user = %s, want staff %s", u.ID, staffID)
	}
	if sess.ImpersonatorID != nil {
		t.Fatal("after stop the session should not be impersonating")
	}

	// Stopping when not impersonating is an error.
	if err := store.StopImpersonation(ctx, withCookie()); err == nil {
		t.Fatal("StopImpersonation should fail when not impersonating")
	}
}

func seedAuthUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, staff bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, is_staff)
		VALUES ($1, $2, 'x', $3)
	`, id, "imp-"+id.String()+"@example.com", staff); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
	return id
}
