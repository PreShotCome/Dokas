package mobileauth_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preshotcome/vesta/internal/account"
	"github.com/preshotcome/vesta/internal/mobileauth"
)

// seed inserts a user + their personal account and returns both IDs. Mobile
// tokens FK to users(id) and accounts(id), so both must exist.
func seed(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (userID, accountID uuid.UUID) {
	t.Helper()
	userID = uuid.New()
	email := "mobile-test+" + userID.String() + "@example.com"
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'x')
	`, userID, email); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID) })

	acct, err := account.NewStore(pool).CreatePersonalAccount(ctx, userID, email)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	return userID, acct.ID
}

func newPool(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, ctx
}

func TestTokenLifecycle(t *testing.T) {
	pool, ctx := newPool(t)
	userID, accountID := seed(t, ctx, pool)
	store := mobileauth.NewStore(pool)

	tok, err := store.Create(ctx, userID, accountID, "Pixel 8", mobileauth.DefaultTTL)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if tok.Secret == "" || tok.ID == uuid.Nil {
		t.Fatal("create returned empty secret/id")
	}

	got, err := store.Authenticate(ctx, tok.Secret)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if got.UserID != userID || got.AccountID != accountID {
		t.Fatalf("authenticate returned (%s,%s), want (%s,%s)", got.UserID, got.AccountID, userID, accountID)
	}

	// Garbage and wrong-prefix tokens are rejected.
	if _, err := store.Authenticate(ctx, "not-a-token"); err != mobileauth.ErrNotFound {
		t.Fatalf("garbage token err = %v, want ErrNotFound", err)
	}

	// Revoked tokens stop authenticating.
	if err := store.Revoke(ctx, userID, tok.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := store.Authenticate(ctx, tok.Secret); err != mobileauth.ErrRevoked {
		t.Fatalf("revoked auth err = %v, want ErrRevoked", err)
	}
}

func TestTokenExpiry(t *testing.T) {
	pool, ctx := newPool(t)
	userID, accountID := seed(t, ctx, pool)
	store := mobileauth.NewStore(pool)

	tok, err := store.Create(ctx, userID, accountID, "", -time.Minute) // negative TTL falls back to default
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Default TTL means it authenticates fine; then force-expire it via SQL.
	if _, err := store.Authenticate(ctx, tok.Secret); err != nil {
		t.Fatalf("authenticate fresh: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE mobile_auth_tokens SET expires_at = now() - interval '1 hour' WHERE id = $1`, tok.ID); err != nil {
		t.Fatalf("force expire: %v", err)
	}
	if _, err := store.Authenticate(ctx, tok.Secret); err != mobileauth.ErrExpired {
		t.Fatalf("expired auth err = %v, want ErrExpired", err)
	}
}

func TestMFAChallengeConsumeOnce(t *testing.T) {
	pool, ctx := newPool(t)
	userID, accountID := seed(t, ctx, pool)
	store := mobileauth.NewStore(pool)

	id, err := store.CreateChallenge(ctx, userID, accountID, "iPhone")
	if err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	ch, err := store.ConsumeChallenge(ctx, id)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if ch.UserID != userID || ch.AccountID != accountID || ch.DeviceName != "iPhone" {
		t.Fatalf("consume returned %+v", ch)
	}
	// A challenge is single-use: the second consume finds nothing.
	if _, err := store.ConsumeChallenge(ctx, id); err != mobileauth.ErrNotFound {
		t.Fatalf("second consume err = %v, want ErrNotFound", err)
	}
}
