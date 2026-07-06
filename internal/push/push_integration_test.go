// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package push_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preshotcome/dokaz/internal/account"
	"github.com/preshotcome/dokaz/internal/drill"
	"github.com/preshotcome/dokaz/internal/heartbeat"
	"github.com/preshotcome/dokaz/internal/push"
)

func seed(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (userID, accountID uuid.UUID) {
	t.Helper()
	userID = uuid.New()
	email := "push-test+" + userID.String() + "@example.com"
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, password_hash) VALUES ($1,$2,'x')`, userID, email); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID) })
	acct, err := account.NewStore(pool).CreatePersonalAccount(ctx, userID, email)
	if err != nil {
		t.Fatalf("account: %v", err)
	}
	return userID, acct.ID
}

func pgPool(t *testing.T) (*pgxpool.Pool, context.Context) {
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

func TestDeviceRegistryUpsert(t *testing.T) {
	pool, ctx := pgPool(t)
	userID, accountID := seed(t, ctx, pool)
	store := push.NewStore(pool)

	id1, err := store.Register(ctx, userID, accountID, "tok-abc", "android")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	// Re-registering the same token upserts (no duplicate row, same id).
	id2, err := store.Register(ctx, userID, accountID, "tok-abc", "android")
	if err != nil {
		t.Fatalf("re-register: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("upsert produced a new row: %s != %s", id1, id2)
	}
	tokens, err := store.TokensForAccount(ctx, accountID)
	if err != nil {
		t.Fatalf("tokens: %v", err)
	}
	if len(tokens) != 1 || tokens[0] != "tok-abc" {
		t.Fatalf("tokens = %v, want [tok-abc]", tokens)
	}
	if err := store.Delete(ctx, userID, id1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if tokens, _ := store.TokensForAccount(ctx, accountID); len(tokens) != 0 {
		t.Fatalf("tokens after delete = %v, want none", tokens)
	}
}

// fakeSender captures the most recent Send call.
type fakeSender struct {
	tokens []string
	notif  push.Notification
}

func (f *fakeSender) Send(_ context.Context, tokens []string, n push.Notification) error {
	f.tokens, f.notif = tokens, n
	return nil
}

func TestHeartbeatNotifierPushesToAccountDevices(t *testing.T) {
	pool, ctx := pgPool(t)
	userID, accountID := seed(t, ctx, pool)
	store := push.NewStore(pool)
	if _, err := store.Register(ctx, userID, accountID, "tok-xyz", "ios"); err != nil {
		t.Fatalf("register: %v", err)
	}

	fake := &fakeSender{}
	n := push.NewHeartbeatNotifier(store, fake, nil)
	hb := heartbeat.Heartbeat{ID: uuid.New(), AccountID: accountID, Name: "nightly dump"}

	if err := n.Notify(ctx, hb, heartbeat.EventDown); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(fake.tokens) != 1 || fake.tokens[0] != "tok-xyz" {
		t.Fatalf("sent to %v, want [tok-xyz]", fake.tokens)
	}
	if fake.notif.Data["type"] != "heartbeat" || fake.notif.Data["id"] != hb.ID.String() {
		t.Fatalf("notif data = %v", fake.notif.Data)
	}
	if fake.notif.Title != "Backup check-in DOWN" {
		t.Fatalf("title = %q", fake.notif.Title)
	}
}

func TestDrillNotifierPushesToAccountDevices(t *testing.T) {
	pool, ctx := pgPool(t)
	userID, accountID := seed(t, ctx, pool)
	store := push.NewStore(pool)
	if _, err := store.Register(ctx, userID, accountID, "tok-drill", "android"); err != nil {
		t.Fatalf("register: %v", err)
	}

	fake := &fakeSender{}
	n := push.NewDrillNotifier(store, fake, nil)
	dr := drill.Drill{ID: uuid.New(), AccountID: accountID}

	if err := n.NotifyDrill(ctx, dr, drill.EventFailed, "assertion_failed"); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(fake.tokens) != 1 || fake.tokens[0] != "tok-drill" {
		t.Fatalf("sent to %v", fake.tokens)
	}
	if fake.notif.Data["type"] != "drill" || fake.notif.Data["id"] != dr.ID.String() ||
		fake.notif.Data["reason"] != "assertion_failed" {
		t.Fatalf("data = %v", fake.notif.Data)
	}
	if fake.notif.Title != "Drill FAILED" {
		t.Fatalf("title = %q", fake.notif.Title)
	}

	// Completed event: no reason in data, friendlier title.
	if err := n.NotifyDrill(ctx, dr, drill.EventCompleted, ""); err != nil {
		t.Fatalf("notify completed: %v", err)
	}
	if fake.notif.Title != "Drill passed" || fake.notif.Data["reason"] != "" {
		t.Fatalf("completed notif wrong: title=%q data=%v", fake.notif.Title, fake.notif.Data)
	}
}
