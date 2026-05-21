package drill_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/preshotcome/anything/internal/account"
	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/auth"
	"github.com/preshotcome/anything/internal/drill"
	"github.com/preshotcome/anything/internal/drill/steps"
	"github.com/preshotcome/anything/internal/evidence"
	"github.com/preshotcome/anything/internal/runner"
)

// seedUserAccount creates a user + personal account, returning both IDs.
func seedUserAccount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accounts *account.Store) (uuid.UUID, uuid.UUID) {
	t.Helper()
	userID := uuid.New()
	hash, err := auth.HashPassword("test-password-12345")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	email := "test+" + userID.String() + "@example.com"
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash) VALUES ($1, $2, $3)
	`, userID, email, hash); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})
	acct, err := accounts.CreatePersonalAccount(ctx, userID, email)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	return userID, acct.ID
}

// TestDrillEndToEnd exercises the full drill workflow against a real Postgres
// (DATABASE_URL must be set). Seeds a user + account + target, enqueues a
// drill, and asserts the PDF lands on disk and every step succeeds.
func TestDrillEndToEnd(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	accounts := account.NewStore(pool)
	userID, accountID := seedUserAccount(t, ctx, pool, accounts)

	fixture := mustAbs(t, "..", "..", "testdata", "fixtures", "tiny.dump")

	drillStore := drill.NewStore(pool)
	target, err := drillStore.CreateTarget(ctx, drill.Target{
		AccountID:       accountID,
		CreatedByUserID: userID,
		Name:            "integration-target",
		SourceKind:      "postgres_dump_local",
		SourceURI:       fixture,
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	rowCountCfg, _ := json.Marshal(map[string]any{"table": "events", "min_rows": 1})
	if _, err := drillStore.CreateAssertion(ctx, target.ID, "row_count", rowCountCfg); err != nil {
		t.Fatalf("create assertion: %v", err)
	}

	evidenceDir := t.TempDir()
	localRunner := runner.NewLocalRunner(dsn)
	workers := river.NewWorkers()
	auditLog := audit.New(pool)

	signer, err := evidence.NewSigner("")
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	evidenceService := evidence.NewService(evidence.NewLocalStore(evidenceDir), signer, pool)

	rc, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues:  map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 4}},
		Workers: workers,
	})
	if err != nil {
		t.Fatalf("river client: %v", err)
	}

	steps.Register(workers, steps.Deps{
		Store:    drillStore,
		Runner:   localRunner,
		Inserter: rc,
		Audit:    auditLog,
		Evidence: evidenceService,
	})

	if err := rc.Start(ctx); err != nil {
		t.Fatalf("river start: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = rc.Stop(stopCtx)
	}()

	orch := drill.NewOrchestrator(drillStore, rc, auditLog)

	drillID, reused, err := drillStore.CreateDrillIdempotent(ctx, accountID, userID, target.ID, uuid.NewString())
	if err != nil {
		t.Fatalf("create drill: %v", err)
	}
	if reused {
		t.Fatalf("brand-new key reported as reused")
	}
	if err := orch.EnqueueDrill(ctx, drillID); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	finalDrill := waitForTerminal(t, ctx, drillStore, drillID, 60*time.Second)

	if finalDrill.Status != drill.StatusSucceeded {
		dumpDrill(t, ctx, drillStore, drillID)
		t.Fatalf("drill did not succeed: status=%s err=%v", finalDrill.Status, deref(finalDrill.Error))
	}
	if finalDrill.AccountID != accountID {
		t.Fatalf("drill account_id mismatch: got %s want %s", finalDrill.AccountID, accountID)
	}
	if finalDrill.EvidencePath == nil || *finalDrill.EvidencePath == "" {
		t.Fatalf("evidence path empty")
	}
	if _, err := os.Stat(*finalDrill.EvidencePath); err != nil {
		t.Fatalf("evidence file not on disk: %v", err)
	}

	stepRows, err := drillStore.ListSteps(ctx, drillID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	if len(stepRows) != len(drill.AllSteps) {
		t.Fatalf("expected %d steps, got %d", len(drill.AllSteps), len(stepRows))
	}
	for _, s := range stepRows {
		if s.Status != drill.StatusSucceeded {
			t.Fatalf("step %s status=%s err=%v", s.Name, s.Status, deref(s.Error))
		}
	}

	ars, err := drillStore.ListAssertions(ctx, drillID)
	if err != nil {
		t.Fatalf("list assertions: %v", err)
	}
	if len(ars) != 1 || !ars[0].Passed {
		t.Fatalf("expected 1 passing assertion, got %+v", ars)
	}

	mustAudit(t, ctx, pool, accountID, "drill.created")
	mustAudit(t, ctx, pool, accountID, "drill.completed")

	// Sandbox database dropped during teardown.
	if finalDrill.SandboxDB == nil || *finalDrill.SandboxDB == "" {
		t.Fatalf("sandbox_db column not populated")
	}
	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)
	`, *finalDrill.SandboxDB).Scan(&exists); err != nil {
		t.Fatalf("pg_database check: %v", err)
	}
	if exists {
		t.Fatalf("sandbox database %q not dropped during teardown", *finalDrill.SandboxDB)
	}

	// Idempotency: a second drill with the same key returns the same drill_id.
	sharedKey := uuid.NewString()
	first, _, err := drillStore.CreateDrillIdempotent(ctx, accountID, userID, target.ID, sharedKey)
	if err != nil {
		t.Fatalf("first idem call: %v", err)
	}
	second, reused2, err := drillStore.CreateDrillIdempotent(ctx, accountID, userID, target.ID, sharedKey)
	if err != nil {
		t.Fatalf("second idem call: %v", err)
	}
	if !reused2 || first != second {
		t.Fatalf("idempotent reuse failed: first=%s second=%s reused=%v", first, second, reused2)
	}
	_, _ = pool.Exec(context.Background(), `DELETE FROM drills WHERE id = $1`, first)
}

// TestCrossAccountIsolation proves account A cannot read account B's drills
// or targets through the account-scoped store methods.
func TestCrossAccountIsolation(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	accounts := account.NewStore(pool)
	drillStore := drill.NewStore(pool)

	userA, accountA := seedUserAccount(t, ctx, pool, accounts)
	userB, accountB := seedUserAccount(t, ctx, pool, accounts)

	fixture := mustAbs(t, "..", "..", "testdata", "fixtures", "tiny.dump")

	// Account A registers a target and a drill.
	targetA, err := drillStore.CreateTarget(ctx, drill.Target{
		AccountID: accountA, CreatedByUserID: userA, Name: "a-target",
		SourceKind: "postgres_dump_local", SourceURI: fixture,
	})
	if err != nil {
		t.Fatalf("create target A: %v", err)
	}
	drillA, _, err := drillStore.CreateDrillIdempotent(ctx, accountA, userA, targetA.ID, uuid.NewString())
	if err != nil {
		t.Fatalf("create drill A: %v", err)
	}

	// Account B sees nothing of A's.
	bTargets, err := drillStore.ListTargets(ctx, accountB)
	if err != nil {
		t.Fatalf("list targets B: %v", err)
	}
	if len(bTargets) != 0 {
		t.Fatalf("account B sees %d targets, expected 0", len(bTargets))
	}
	bDrills, err := drillStore.ListDrills(ctx, accountB, 100)
	if err != nil {
		t.Fatalf("list drills B: %v", err)
	}
	if len(bDrills) != 0 {
		t.Fatalf("account B sees %d drills, expected 0", len(bDrills))
	}

	// Direct account-scoped gets from B must fail with ErrNotFound.
	if _, err := drillStore.GetTarget(ctx, accountB, targetA.ID); err != drill.ErrNotFound {
		t.Fatalf("B GetTarget(A's target): expected ErrNotFound, got %v", err)
	}
	if _, err := drillStore.GetDrill(ctx, accountB, drillA); err != drill.ErrNotFound {
		t.Fatalf("B GetDrill(A's drill): expected ErrNotFound, got %v", err)
	}

	// A still sees its own.
	aTargets, _ := drillStore.ListTargets(ctx, accountA)
	if len(aTargets) != 1 {
		t.Fatalf("account A sees %d targets, expected 1", len(aTargets))
	}
	_ = userB

	_, _ = pool.Exec(context.Background(), `DELETE FROM drills WHERE id = $1`, drillA)
}

func waitForTerminal(t *testing.T, ctx context.Context, s *drill.Store, drillID uuid.UUID, timeout time.Duration) drill.Drill {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var d drill.Drill
	for time.Now().Before(deadline) {
		got, err := s.GetDrillByID(ctx, drillID)
		if err != nil && err != pgx.ErrNoRows {
			t.Fatalf("get drill: %v", err)
		}
		d = got
		if d.Status == drill.StatusSucceeded || d.Status == drill.StatusFailed {
			return d
		}
		time.Sleep(250 * time.Millisecond)
	}
	return d
}

func mustAbs(t *testing.T, parts ...string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join(parts...))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return p
}

func mustAudit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accountID uuid.UUID, action string) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_events WHERE account_id = $1 AND action = $2
	`, accountID, action).Scan(&count); err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected at least one %s audit event for account %s", action, accountID)
	}
}

func dumpDrill(t *testing.T, ctx context.Context, s *drill.Store, drillID uuid.UUID) {
	t.Helper()
	steps, _ := s.ListSteps(ctx, drillID)
	for _, st := range steps {
		t.Logf("step=%s status=%s err=%v", st.Name, st.Status, deref(st.Error))
	}
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
