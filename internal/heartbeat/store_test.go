// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package heartbeat

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testStore opens a pool from DATABASE_URL or skips. Schema is assumed migrated
// (CI runs `migrate up` before tests).
func testStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return NewStore(pool), pool
}

// seedAccount inserts a bare user + account so heartbeat FKs resolve, and
// returns both IDs.
func seedAccount(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (accountID, userID uuid.UUID) {
	t.Helper()
	userID = uuid.New()
	accountID = uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'x')
	`, userID, "hb-test+"+userID.String()+"@example.com"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, name, slug) VALUES ($1, $2, $3)
	`, accountID, "hb-test", "hb-"+accountID.String()[:8]); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE id = $1`, accountID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})
	return accountID, userID
}

func newMonitor(t *testing.T, ctx context.Context, s *Store, acct, user uuid.UUID, period, grace int) Heartbeat {
	t.Helper()
	hb, err := s.Create(ctx, Heartbeat{
		AccountID: acct, CreatedByUserID: user, Name: "nightly backup",
		PeriodSeconds: period, GraceSeconds: grace,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return hb
}

func TestRecordPingTransitions(t *testing.T) {
	s, pool := testStore(t)
	ctx := context.Background()
	acct, user := seedAccount(t, ctx, pool)
	hb := newMonitor(t, ctx, s, acct, user, 3600, 60)

	if hb.Status != StatusNew {
		t.Fatalf("fresh monitor status = %s, want new", hb.Status)
	}

	// new -> up is a transition (first contact).
	got, transitioned, err := s.RecordPing(ctx, hb.PingToken, KindPing, "1.2.3.4", "curl")
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	if !transitioned || got.Status != StatusUp {
		t.Fatalf("first ping: transitioned=%v status=%s, want true/up", transitioned, got.Status)
	}
	if got.LastPingAt == nil {
		t.Fatal("first ping did not set last_ping_at")
	}

	// up -> up is NOT a transition (routine ping, no webhook).
	_, transitioned, err = s.RecordPing(ctx, hb.PingToken, KindPing, "", "")
	if err != nil {
		t.Fatalf("ping 2: %v", err)
	}
	if transitioned {
		t.Fatal("routine ping reported a transition")
	}

	// explicit fail flips to down (a transition).
	got, transitioned, err = s.RecordPing(ctx, hb.PingToken, KindFail, "", "")
	if err != nil {
		t.Fatalf("fail: %v", err)
	}
	if !transitioned || got.Status != StatusDown {
		t.Fatalf("fail: transitioned=%v status=%s, want true/down", transitioned, got.Status)
	}

	// down -> up on recovery (a transition).
	got, transitioned, err = s.RecordPing(ctx, hb.PingToken, KindPing, "", "")
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if !transitioned || got.Status != StatusUp {
		t.Fatalf("recover: transitioned=%v status=%s, want true/up", transitioned, got.Status)
	}

	// a start signal records but never transitions.
	_, transitioned, err = s.RecordPing(ctx, hb.PingToken, KindStart, "", "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if transitioned {
		t.Fatal("start signal reported a transition")
	}

	// The pings were all logged (ping, ping, fail, ping, start = 5).
	pings, err := s.ListPings(ctx, hb.ID, 100)
	if err != nil {
		t.Fatalf("list pings: %v", err)
	}
	if len(pings) != 5 {
		t.Fatalf("logged %d pings, want 5", len(pings))
	}
}

func TestRecordPingUnknownToken(t *testing.T) {
	s, pool := testStore(t)
	ctx := context.Background()
	seedAccount(t, ctx, pool)
	if _, _, err := s.RecordPing(ctx, "nope-not-a-token", KindPing, "", ""); err != ErrNotFound {
		t.Fatalf("unknown token err = %v, want ErrNotFound", err)
	}
}

func TestMarkOverdueDown(t *testing.T) {
	s, pool := testStore(t)
	ctx := context.Background()
	acct, user := seedAccount(t, ctx, pool)
	hb := newMonitor(t, ctx, s, acct, user, 60, 0)

	// Force the deadline into the past so it's overdue now.
	if _, err := pool.Exec(ctx, `
		UPDATE heartbeats SET expected_by = now() - interval '1 hour' WHERE id = $1
	`, hb.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	downed, err := s.MarkOverdueDown(ctx)
	if err != nil {
		t.Fatalf("mark overdue: %v", err)
	}
	if !containsID(downed, hb.ID) {
		t.Fatalf("overdue monitor not flipped: got %d rows", len(downed))
	}

	// Second pass is idempotent — already-down rows are not re-returned, so the
	// sweeper can't double-alert on the same outage.
	again, err := s.MarkOverdueDown(ctx)
	if err != nil {
		t.Fatalf("mark overdue 2: %v", err)
	}
	if containsID(again, hb.ID) {
		t.Fatal("already-down monitor flipped again")
	}

	got, err := s.Get(ctx, acct, hb.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != StatusDown {
		t.Fatalf("status = %s, want down", got.Status)
	}
}

func TestPausedMonitorNotSwept(t *testing.T) {
	s, pool := testStore(t)
	ctx := context.Background()
	acct, user := seedAccount(t, ctx, pool)
	hb := newMonitor(t, ctx, s, acct, user, 60, 0)
	if err := s.Pause(ctx, acct, hb.ID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	// Even with a backdated deadline, a paused monitor (expected_by NULL) is
	// excluded from the sweep.
	if _, err := pool.Exec(ctx, `
		UPDATE heartbeats SET expected_by = now() - interval '1 hour' WHERE id = $1
	`, hb.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	downed, err := s.MarkOverdueDown(ctx)
	if err != nil {
		t.Fatalf("mark overdue: %v", err)
	}
	if containsID(downed, hb.ID) {
		t.Fatal("paused monitor was swept down")
	}
}

func TestCrossAccountIsolation(t *testing.T) {
	s, pool := testStore(t)
	ctx := context.Background()
	acctA, userA := seedAccount(t, ctx, pool)
	acctB, _ := seedAccount(t, ctx, pool)
	hb := newMonitor(t, ctx, s, acctA, userA, 60, 0)

	if _, err := s.Get(ctx, acctB, hb.ID); err != ErrNotFound {
		t.Fatalf("cross-account Get err = %v, want ErrNotFound", err)
	}
	if err := s.Delete(ctx, acctB, hb.ID); err != ErrNotFound {
		t.Fatalf("cross-account Delete err = %v, want ErrNotFound", err)
	}
	// The owning account still sees it.
	if _, err := s.Get(ctx, acctA, hb.ID); err != nil {
		t.Fatalf("owner Get: %v", err)
	}
}

func containsID(hbs []Heartbeat, id uuid.UUID) bool {
	for _, h := range hbs {
		if h.ID == id {
			return true
		}
	}
	return false
}
