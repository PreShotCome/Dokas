package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMFAEnableDisable(t *testing.T) {
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

	userID := seedAuthUser(t, ctx, pool, false)
	store := NewStore(pool, 14*24*time.Hour, 30*24*time.Hour, false)

	secret, _ := GenerateTOTPSecret()
	if err := store.EnableMFA(ctx, userID, secret); err != nil {
		t.Fatalf("EnableMFA: %v", err)
	}
	got, err := store.TOTPSecret(ctx, userID)
	if err != nil || got != secret {
		t.Fatalf("TOTPSecret = %q, %v; want %q", got, err, secret)
	}
	if u, _ := store.LoadUserByID(ctx, userID); !u.MFAEnabled {
		t.Fatal("user should report MFAEnabled after EnableMFA")
	}

	// Recovery codes: store a set, consume one, then prove single-use.
	codes, _ := GenerateRecoveryCodes()
	if err := store.ReplaceRecoveryCodes(ctx, userID, codes); err != nil {
		t.Fatalf("ReplaceRecoveryCodes: %v", err)
	}
	if ok, err := store.ConsumeRecoveryCode(ctx, userID, codes[0]); err != nil || !ok {
		t.Fatalf("first consume = %v, %v; want true", ok, err)
	}
	if ok, _ := store.ConsumeRecoveryCode(ctx, userID, codes[0]); ok {
		t.Fatal("a recovery code must be single-use")
	}
	if ok, _ := store.ConsumeRecoveryCode(ctx, userID, "bogus-code"); ok {
		t.Fatal("an unknown recovery code must not consume")
	}

	// Disable clears the secret, the flag, and the recovery codes.
	if err := store.DisableMFA(ctx, userID); err != nil {
		t.Fatalf("DisableMFA: %v", err)
	}
	if u, _ := store.LoadUserByID(ctx, userID); u.MFAEnabled {
		t.Fatal("user should not report MFAEnabled after DisableMFA")
	}
	if ok, _ := store.ConsumeRecoveryCode(ctx, userID, codes[1]); ok {
		t.Fatal("recovery codes should be gone after disable")
	}
}

func TestMFAPendingSessionFlow(t *testing.T) {
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

	userID := seedAuthUser(t, ctx, pool, false)
	store := NewStore(pool, 14*24*time.Hour, 30*24*time.Hour, false)

	rec := httptest.NewRecorder()
	if err := store.CreatePending(ctx, rec, userID); err != nil {
		t.Fatalf("CreatePending: %v", err)
	}
	cookie := rec.Result().Cookies()[0]
	withCookie := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(cookie)
		return r
	}

	// A pending session is reported as such, and PendingMFAUser resolves it.
	_, sess, err := store.Lookup(ctx, withCookie())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !sess.MFAPending {
		t.Fatal("a pending session should report MFAPending")
	}
	u, err := store.PendingMFAUser(ctx, withCookie())
	if err != nil || u.ID != userID {
		t.Fatalf("PendingMFAUser = %v, %v; want user %s", u, err, userID)
	}

	// Completing MFA promotes the session to fully authenticated.
	if err := store.CompleteMFA(ctx, withCookie()); err != nil {
		t.Fatalf("CompleteMFA: %v", err)
	}
	_, sess, err = store.Lookup(ctx, withCookie())
	if err != nil {
		t.Fatalf("Lookup after CompleteMFA: %v", err)
	}
	if sess.MFAPending {
		t.Fatal("session should not be pending after CompleteMFA")
	}
	if _, err := store.PendingMFAUser(ctx, withCookie()); err == nil {
		t.Fatal("PendingMFAUser should not resolve a completed session")
	}
}
