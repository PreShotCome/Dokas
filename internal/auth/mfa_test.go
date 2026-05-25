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
	if err := store.EnableMFA(ctx, userID, secret, 0); err != nil {
		t.Fatalf("EnableMFA: %v", err)
	}
	// Confirm the secret round-tripped by verifying a code against it.
	code, _ := TOTPCode(secret, time.Now())
	if ok, err := store.VerifyAndConsumeTOTP(ctx, userID, code); err != nil || !ok {
		t.Fatalf("a code for the stored secret should verify: ok=%v err=%v", ok, err)
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

func TestVerifyAndConsumeTOTPRejectsReplay(t *testing.T) {
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
	if err := store.EnableMFA(ctx, userID, secret, 0); err != nil {
		t.Fatalf("EnableMFA: %v", err)
	}
	code, _ := TOTPCode(secret, time.Now())

	if ok, err := store.VerifyAndConsumeTOTP(ctx, userID, code); err != nil || !ok {
		t.Fatalf("first use of a valid code = %v, %v; want true", ok, err)
	}
	// The same code is still inside its ~90s window, but replaying it must fail.
	if ok, err := store.VerifyAndConsumeTOTP(ctx, userID, code); err != nil || ok {
		t.Fatalf("replayed code = %v, %v; want false", ok, err)
	}
}

// N11: the TOTP code used to confirm enrollment must NOT be reusable as the
// first /login/mfa code, even though it's still inside its validity window.
// EnableMFA seeds totp_last_used_counter with the confirm code's counter so
// VerifyAndConsumeTOTP rejects it on replay.
func TestEnableMFASealsConfirmCounter(t *testing.T) {
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
	now := time.Now()
	code, _ := TOTPCode(secret, now)
	counter, ok := VerifyTOTPWithCounter(secret, code, now)
	if !ok {
		t.Fatal("VerifyTOTPWithCounter on a freshly-minted code should succeed")
	}
	if err := store.EnableMFA(ctx, userID, secret, counter); err != nil {
		t.Fatalf("EnableMFA: %v", err)
	}
	// The same code is still inside its window — but the confirmation
	// counter is sealed, so replaying it at /login/mfa must fail.
	if ok, err := store.VerifyAndConsumeTOTP(ctx, userID, code); err != nil || ok {
		t.Fatalf("enrollment code replayed: got ok=%v err=%v, want false/nil", ok, err)
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

	// Completing MFA rotates the session: the old token is destroyed and a
	// fresh, fully authenticated session is issued under a new cookie.
	rec2 := httptest.NewRecorder()
	if err := store.CompleteMFA(ctx, rec2, withCookie()); err != nil {
		t.Fatalf("CompleteMFA: %v", err)
	}
	if _, _, err := store.Lookup(ctx, withCookie()); err == nil {
		t.Fatal("the pre-MFA session should be destroyed after CompleteMFA")
	}

	newCookie := rec2.Result().Cookies()[0]
	if newCookie.Value == cookie.Value {
		t.Fatal("CompleteMFA must issue a new session token")
	}
	newReq := httptest.NewRequest(http.MethodGet, "/", nil)
	newReq.AddCookie(newCookie)
	u2, sess2, err := store.Lookup(ctx, newReq)
	if err != nil {
		t.Fatalf("Lookup of the rotated session: %v", err)
	}
	if sess2.MFAPending {
		t.Fatal("the rotated session must not be pending")
	}
	if u2.ID != userID {
		t.Fatalf("rotated session user = %s, want %s", u2.ID, userID)
	}
}
