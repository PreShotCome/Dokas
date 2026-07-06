// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package audit

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestListForAccount records a few events and reads them back newest-first,
// then walks the keyset cursor. Needs Postgres; skips without DATABASE_URL.
func TestListForAccount(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	ctx := context.Background()

	userID := uuid.New()
	accountID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id,email,password_hash) VALUES ($1,$2,'x')`,
		userID, "audit-"+userID.String()+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO accounts (id,name,slug,plan) VALUES ($1,'a','audit-'||$2,'pro')`,
		accountID, accountID.String()[:8]); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, accountID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})

	l := New(pool)
	for i, action := range []string{"first.event", "second.event", "third.event"} {
		if err := l.Record(ctx, Event{
			AccountID: &accountID, ActorID: &userID, Action: action,
			TargetKind: "thing", TargetID: "t" + string(rune('0'+i)),
			IP: "203.0.113.7", Metadata: map[string]any{"n": i},
		}); err != nil {
			t.Fatalf("record %s: %v", action, err)
		}
	}

	// Newest-first: the last-recorded event leads.
	page, err := l.ListForAccount(ctx, accountID, 2, 0)
	if err != nil {
		t.Fatalf("list page 1: %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("page 1 len = %d, want 2", len(page))
	}
	if page[0].Action != "third.event" {
		t.Fatalf("newest action = %q, want third.event", page[0].Action)
	}
	if page[0].ActorEmail == "" {
		t.Error("actor email should be resolved from the users join")
	}
	if page[0].IP != "203.0.113.7" {
		t.Errorf("ip = %q, want 203.0.113.7", page[0].IP)
	}

	// Keyset cursor: the next page yields the oldest, with no overlap.
	page2, err := l.ListForAccount(ctx, accountID, 2, page[len(page)-1].ID)
	if err != nil {
		t.Fatalf("list page 2: %v", err)
	}
	if len(page2) != 1 {
		t.Fatalf("page 2 len = %d, want 1", len(page2))
	}
	if page2[0].Action != "first.event" {
		t.Fatalf("oldest action = %q, want first.event", page2[0].Action)
	}
}
