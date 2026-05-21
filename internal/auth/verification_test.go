package auth

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEmailVerificationLifecycle(t *testing.T) {
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

	// A fresh user is unverified.
	u, err := store.LoadUserByID(ctx, userID)
	if err != nil {
		t.Fatalf("load user: %v", err)
	}
	if u.EmailVerified {
		t.Fatal("a new user should be unverified")
	}

	// Issue a token and verify with it.
	token, err := store.CreateVerificationToken(ctx, userID)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	got, err := store.VerifyEmail(ctx, token)
	if err != nil {
		t.Fatalf("verify email: %v", err)
	}
	if got != userID {
		t.Fatalf("verified user = %s, want %s", got, userID)
	}
	u, _ = store.LoadUserByID(ctx, userID)
	if !u.EmailVerified {
		t.Fatal("user should be verified after VerifyEmail")
	}

	// The token is single-use.
	if _, err := store.VerifyEmail(ctx, token); err != ErrVerificationGone {
		t.Fatalf("reused token: got %v, want ErrVerificationGone", err)
	}

	// An unknown token is rejected.
	if _, err := store.VerifyEmail(ctx, "not-a-real-token"); err != ErrVerificationInvalid {
		t.Fatalf("unknown token: got %v, want ErrVerificationInvalid", err)
	}
}

func TestVerifyEmailExpired(t *testing.T) {
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

	// Insert a token whose expiry is already in the past.
	raw, hash, err := generateToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO email_verification_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, now() - interval '1 hour')
	`, userID, hash); err != nil {
		t.Fatalf("insert expired token: %v", err)
	}
	if _, err := store.VerifyEmail(ctx, raw); err != ErrVerificationGone {
		t.Fatalf("expired token: got %v, want ErrVerificationGone", err)
	}
}
