package drill_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/auth"
	"github.com/preshotcome/anything/internal/drill"
	"github.com/preshotcome/anything/internal/drill/steps"
	"github.com/preshotcome/anything/internal/runner"
)

// TestDrillEndToEnd exercises the full drill workflow against a real Postgres
// (DATABASE_URL must be set). It seeds a user + target, enqueues a drill, and
// asserts the PDF lands on disk and every step succeeds.
//
// Skips when DATABASE_URL is unset so plain `go test ./...` against no-DB
// boxes still passes.
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

	// Seed a user directly (bypass signup form).
	userID := uuid.New()
	hash, err := auth.HashPassword("test-password-12345")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash)
		VALUES ($1, $2, $3)
	`, userID, "test+"+userID.String()+"@example.com", hash); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	// Resolve the fixture path relative to this test file so the test passes
	// regardless of CWD.
	fixture := mustAbs(t, "..", "..", "testdata", "fixtures", "tiny.dump")

	drillStore := drill.NewStore(pool)
	target, err := drillStore.CreateTarget(ctx, drill.Target{
		UserID:           userID,
		Name:             "integration-target",
		SourceKind:       "postgres_dump_local",
		SourceURI:        fixture,
		AssertionTable:   "events",
		AssertionMinRows: 1,
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}

	evidenceDir := t.TempDir()
	localRunner := runner.NewLocalRunner(dsn)
	workers := river.NewWorkers()

	auditLog := audit.New(pool)

	rc, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues:  map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 4}},
		Workers: workers,
	})
	if err != nil {
		t.Fatalf("river client: %v", err)
	}

	steps.Register(workers, steps.Deps{
		Store:       drillStore,
		Runner:      localRunner,
		Inserter:    rc,
		Audit:       auditLog,
		EvidenceDir: evidenceDir,
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

	drillID, reused, err := drillStore.CreateDrillIdempotent(ctx, userID, target.ID, uuid.NewString())
	if err != nil {
		t.Fatalf("create drill: %v", err)
	}
	if reused {
		t.Fatalf("brand-new key reported as reused")
	}
	if err := orch.EnqueueDrill(ctx, drillID); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Poll for terminal status.
	deadline := time.Now().Add(60 * time.Second)
	var finalDrill drill.Drill
	for time.Now().Before(deadline) {
		d, err := drillStore.GetDrillByID(ctx, drillID)
		if err != nil && err != pgx.ErrNoRows {
			t.Fatalf("get drill: %v", err)
		}
		finalDrill = d
		if d.Status == drill.StatusSucceeded || d.Status == drill.StatusFailed {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}

	if finalDrill.Status != drill.StatusSucceeded {
		dumpDrill(t, ctx, drillStore, drillID)
		t.Fatalf("drill did not succeed: status=%s err=%v", finalDrill.Status, deref(finalDrill.Error))
	}

	if finalDrill.EvidencePath == nil || *finalDrill.EvidencePath == "" {
		t.Fatalf("evidence path empty")
	}
	if _, err := os.Stat(*finalDrill.EvidencePath); err != nil {
		t.Fatalf("evidence file not on disk: %v", err)
	}

	// Verify every step succeeded.
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

	// Verify assertion result is recorded and passed.
	ars, err := drillStore.ListAssertions(ctx, drillID)
	if err != nil {
		t.Fatalf("list assertions: %v", err)
	}
	if len(ars) != 1 {
		t.Fatalf("expected 1 assertion, got %d", len(ars))
	}
	if !ars[0].Passed {
		t.Fatalf("assertion did not pass: %s", string(ars[0].Actual))
	}

	// Verify audit events for drill.created and drill.completed exist.
	mustAudit(t, ctx, pool, userID, "drill.created")
	mustAudit(t, ctx, pool, userID, "drill.completed")

	// Verify the sandbox database has been dropped during teardown.
	var sandboxName string
	if finalDrill.SandboxDB != nil {
		sandboxName = *finalDrill.SandboxDB
	}
	if sandboxName == "" {
		t.Fatalf("sandbox_db column not populated")
	}
	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)
	`, sandboxName).Scan(&exists); err != nil {
		t.Fatalf("pg_database check: %v", err)
	}
	if exists {
		t.Fatalf("sandbox database %q was not dropped during teardown", sandboxName)
	}

	// Idempotency: a second drill with the same key returns the same drill_id.
	sharedKey := uuid.NewString()
	first, _, err := drillStore.CreateDrillIdempotent(ctx, userID, target.ID, sharedKey)
	if err != nil {
		t.Fatalf("first idem call: %v", err)
	}
	second, reused2, err := drillStore.CreateDrillIdempotent(ctx, userID, target.ID, sharedKey)
	if err != nil {
		t.Fatalf("second idem call: %v", err)
	}
	if !reused2 {
		t.Fatalf("expected reuse on duplicate idempotency key")
	}
	if first != second {
		t.Fatalf("idempotent reuse returned different drill_id: %s vs %s", first, second)
	}
	// Clean up the orphan idempotency-test drill (it was never enqueued).
	_, _ = pool.Exec(context.Background(), `DELETE FROM drills WHERE id = $1`, first)
}

func mustAbs(t *testing.T, parts ...string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join(parts...))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return p
}

func mustAudit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, action string) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_events WHERE actor_id = $1 AND action = $2
	`, userID, action).Scan(&count); err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected at least one %s audit event", action)
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
