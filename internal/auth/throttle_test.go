package auth

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLoginThrottle(t *testing.T) {
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

	// Unique email so the test is isolated from other rows.
	email := "throttle-" + uuid.NewString() + "@example.com"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM login_attempts WHERE email = $1`, email)
	})

	throttle := NewLoginThrottle(pool, 3, time.Hour)

	// Fresh email: not locked.
	if st, err := throttle.Check(ctx, email); err != nil || st.Locked {
		t.Fatalf("fresh email should not be locked: %+v err=%v", st, err)
	}

	// Two failures: still under the limit of 3.
	for i := 0; i < 2; i++ {
		if err := throttle.Record(ctx, email, "1.2.3.4", false); err != nil {
			t.Fatalf("record fail: %v", err)
		}
	}
	if st, _ := throttle.Check(ctx, email); st.Locked {
		t.Fatal("2 failures should not lock with a limit of 3")
	}

	// Third failure trips the lock.
	if err := throttle.Record(ctx, email, "1.2.3.4", false); err != nil {
		t.Fatalf("record fail: %v", err)
	}
	st, err := throttle.Check(ctx, email)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !st.Locked {
		t.Fatal("3 failures should lock the email")
	}
	if st.RetryAfter <= 0 {
		t.Fatalf("locked state should report a positive RetryAfter, got %v", st.RetryAfter)
	}

	// A successful login clears the streak: failures before it no longer count.
	if err := throttle.Record(ctx, email, "1.2.3.4", true); err != nil {
		t.Fatalf("record success: %v", err)
	}
	if st, _ := throttle.Check(ctx, email); st.Locked {
		t.Fatal("a successful login should clear the lock")
	}
}
