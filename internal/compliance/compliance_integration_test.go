package compliance

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/evidence"
)

// seedFullAccount builds a user + account + target + drill + evidence so the
// export and purge paths have something to act on.
func seedFullAccount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ev *evidence.Service) (accountID, userID, drillID uuid.UUID, evidenceKey string) {
	t.Helper()
	userID = uuid.New()
	accountID = uuid.New()
	targetID := uuid.New()
	drillID = uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO users (id,email,password_hash) VALUES ($1,$2,'x')`,
		userID, "cmp-"+userID.String()+"@example.com"); err != nil {
		t.Fatalf("user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id,name,slug) VALUES ($1,'cmp','cmp-'||$2)`,
		accountID, accountID.String()[:8]); err != nil {
		t.Fatalf("account: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO memberships (account_id,user_id,role) VALUES ($1,$2,'owner')`,
		accountID, userID); err != nil {
		t.Fatalf("membership: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO database_targets (id,account_id,created_by_user_id,name,source_kind,source_uri,assertion_table)
		VALUES ($1,$2,$3,'prod','postgres_dump_local','/x','events')`,
		targetID, accountID, userID); err != nil {
		t.Fatalf("target: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO drills (id,target_id,account_id,created_by_user_id,status)
		VALUES ($1,$2,$3,$4,'succeeded')`,
		drillID, targetID, accountID, userID); err != nil {
		t.Fatalf("drill: %v", err)
	}
	key, err := ev.Finalize(ctx, drillID, []byte("%PDF evidence"))
	if err != nil {
		t.Fatalf("finalize evidence: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE drills SET evidence_path=$2 WHERE id=$1`, drillID, key); err != nil {
		t.Fatalf("set evidence_path: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, accountID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	return accountID, userID, drillID, key
}

func newEvidence(t *testing.T, pool *pgxpool.Pool) *evidence.Service {
	t.Helper()
	signer, err := evidence.NewSigner("")
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	return evidence.NewService(evidence.NewLocalStore(t.TempDir()), signer, pool)
}

func TestExportContainsAllEntities(t *testing.T) {
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

	ev := newEvidence(t, pool)
	accountID, _, drillID, _ := seedFullAccount(t, ctx, pool, ev)

	var buf bytes.Buffer
	if err := NewExporter(pool).Export(ctx, accountID, &buf); err != nil {
		t.Fatalf("Export: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("export is not valid JSON: %v", err)
	}
	for _, section := range []string{
		"account", "members", "database_targets", "drills",
		"evidence_signatures", "audit_events",
	} {
		if _, ok := doc[section]; !ok {
			t.Errorf("export missing section %q", section)
		}
	}
	// The drill we seeded must appear.
	drills, _ := doc["drills"].([]any)
	if len(drills) != 1 {
		t.Fatalf("export drills = %d, want 1", len(drills))
	}
	row, _ := drills[0].(map[string]any)
	if row["id"] != drillID.String() {
		t.Fatalf("exported drill id = %v, want %s", row["id"], drillID)
	}
}

func TestSoftDeleteThenHardDelete(t *testing.T) {
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

	ev := newEvidence(t, pool)
	accountID, userID, drillID, evidenceKey := seedFullAccount(t, ctx, pool, ev)

	// Zero grace so the hard delete is immediately eligible.
	purger := NewPurger(pool, ev, audit.New(pool), 0)

	if _, err := purger.SoftDelete(ctx, accountID, userID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	// Soft delete sets deleted_at; the account row still exists.
	var deletedAt *string
	if err := pool.QueryRow(ctx, `SELECT deleted_at::text FROM accounts WHERE id=$1`, accountID).Scan(&deletedAt); err != nil {
		t.Fatalf("read account after soft delete: %v", err)
	}
	if deletedAt == nil {
		t.Fatal("soft delete did not set deleted_at")
	}

	// Hard delete: removes the account (cascades) and shreds evidence.
	if err := purger.HardDelete(ctx, accountID); err != nil {
		t.Fatalf("HardDelete: %v", err)
	}

	var accountCount, drillCount, sigCount int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE id=$1`, accountID).Scan(&accountCount)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM drills WHERE id=$1`, drillID).Scan(&drillCount)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM evidence_signatures WHERE drill_id=$1`, drillID).Scan(&sigCount)
	if accountCount != 0 || drillCount != 0 || sigCount != 0 {
		t.Fatalf("hard delete left rows: account=%d drill=%d sig=%d", accountCount, drillCount, sigCount)
	}
	if _, err := os.Stat(evidenceKey); !os.IsNotExist(err) {
		t.Fatalf("evidence file not shredded, stat err = %v", err)
	}

	// The user survives (could belong to other accounts).
	var userCount int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id=$1`, userID).Scan(&userCount)
	if userCount != 1 {
		t.Fatalf("user row count = %d, want 1 (users outlive account deletion)", userCount)
	}
}

func TestHardDeleteRefusesLiveAccount(t *testing.T) {
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

	ev := newEvidence(t, pool)
	accountID, _, _, _ := seedFullAccount(t, ctx, pool, ev)

	purger := NewPurger(pool, ev, audit.New(pool), DefaultGracePeriod)
	// Never soft-deleted → not eligible.
	err = purger.HardDelete(ctx, accountID)
	if err == nil {
		t.Fatal("HardDelete should refuse a live (non-soft-deleted) account")
	}
}
