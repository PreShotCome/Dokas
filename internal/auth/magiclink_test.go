package auth

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMagicLinkLifecycle(t *testing.T) {
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

	token, err := store.CreateMagicLinkToken(ctx, userID)
	if err != nil {
		t.Fatalf("CreateMagicLinkToken: %v", err)
	}
	got, err := store.ConsumeMagicLink(ctx, token)
	if err != nil {
		t.Fatalf("ConsumeMagicLink: %v", err)
	}
	if got != userID {
		t.Fatalf("consumed user = %s, want %s", got, userID)
	}

	// Single-use.
	if _, err := store.ConsumeMagicLink(ctx, token); err != ErrMagicLinkGone {
		t.Fatalf("reused token: got %v, want ErrMagicLinkGone", err)
	}
	// Unknown token.
	if _, err := store.ConsumeMagicLink(ctx, "not-a-real-token"); err != ErrMagicLinkInvalid {
		t.Fatalf("unknown token: got %v, want ErrMagicLinkInvalid", err)
	}
}

func TestMagicLinkExpired(t *testing.T) {
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

	raw, hash, err := generateToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO magic_link_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, now() - interval '1 minute')
	`, userID, hash); err != nil {
		t.Fatalf("insert expired token: %v", err)
	}
	if _, err := store.ConsumeMagicLink(ctx, raw); err != ErrMagicLinkGone {
		t.Fatalf("expired token: got %v, want ErrMagicLinkGone", err)
	}
}
