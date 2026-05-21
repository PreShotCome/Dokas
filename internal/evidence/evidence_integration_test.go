package evidence

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedDrill inserts the minimal user/account/target/drill chain so an
// evidence_signatures row (FK → drills) can be created.
func seedDrill(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	userID := uuid.New()
	accountID := uuid.New()
	targetID := uuid.New()
	drillID := uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, password_hash) VALUES ($1,$2,'x')`,
		userID, "ev-"+userID.String()+"@example.com"); err != nil {
		t.Fatalf("user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id, name, slug) VALUES ($1,'ev','ev-'||$2)`,
		accountID, accountID.String()[:8]); err != nil {
		t.Fatalf("account: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO database_targets (id, account_id, created_by_user_id, name, source_kind, source_uri, assertion_table)
		VALUES ($1,$2,$3,'t','postgres_dump_local','/x','events')`,
		targetID, accountID, userID); err != nil {
		t.Fatalf("target: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO drills (id, target_id, account_id, created_by_user_id, status)
		VALUES ($1,$2,$3,$4,'succeeded')`,
		drillID, targetID, accountID, userID); err != nil {
		t.Fatalf("drill: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, accountID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	return drillID
}

func TestServiceFinalizeAndVerify(t *testing.T) {
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

	drillID := seedDrill(t, ctx, pool)
	dir := t.TempDir()
	signer, _ := NewSigner("")
	svc := NewService(NewLocalStore(dir), signer, pool)

	pdf := []byte("%PDF-1.3 evidence body")
	key, err := svc.Finalize(ctx, drillID, pdf)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	// Finalize records evidence_path nowhere — the drill row update is the
	// caller's job — so set it here for Verify's join-free path.
	if _, err := pool.Exec(ctx, `UPDATE drills SET evidence_path=$2 WHERE id=$1`, drillID, key); err != nil {
		t.Fatalf("set evidence_path: %v", err)
	}

	res, err := svc.Verify(ctx, drillID, key)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.Signed || !res.Valid {
		t.Fatalf("expected signed+valid, got %+v", res)
	}

	// Tamper with the stored file on disk; Verify must now fail.
	if err := os.WriteFile(key, []byte("tampered"), 0o644); err != nil {
		t.Fatalf("tamper write: %v", err)
	}
	res, err = svc.Verify(ctx, drillID, key)
	if err != nil {
		t.Fatalf("Verify after tamper: %v", err)
	}
	if res.Valid {
		t.Fatal("Verify reported valid for a tampered evidence file")
	}
	if res.Reason == "" {
		t.Fatal("invalid result should carry a Reason")
	}
}

func TestPurgeExpiredRespectsRetention(t *testing.T) {
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

	drillID := seedDrill(t, ctx, pool)
	dir := t.TempDir()
	signer, _ := NewSigner("")
	svc := NewService(NewLocalStore(dir), signer, pool)

	key, err := svc.Finalize(ctx, drillID, []byte("evidence"))
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE drills SET evidence_path=$2 WHERE id=$1`, drillID, key); err != nil {
		t.Fatalf("set evidence_path: %v", err)
	}

	// retain_until is 7 years out — a purge must NOT touch it.
	n, err := svc.PurgeExpired(ctx)
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if n != 0 {
		t.Fatalf("purged %d rows that are still within retention", n)
	}
	if _, err := os.Stat(key); err != nil {
		t.Fatalf("evidence file removed before retain_until: %v", err)
	}

	// Backdate retain_until into the past — now it's eligible.
	if _, err := pool.Exec(ctx, `
		UPDATE evidence_signatures SET retain_until = now() - interval '1 day' WHERE drill_id = $1
	`, drillID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	n, err = svc.PurgeExpired(ctx)
	if err != nil {
		t.Fatalf("PurgeExpired (expired): %v", err)
	}
	if n != 1 {
		t.Fatalf("purged %d rows, want 1", n)
	}
	if _, err := os.Stat(key); !os.IsNotExist(err) {
		t.Fatalf("evidence file should be gone after retention expiry, stat err = %v", err)
	}
	if _, err := svc.GetSignature(ctx, drillID); err != ErrNoSignature {
		t.Fatalf("signature row should be gone, got %v", err)
	}
	_ = time.Now
}
