package handlers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preshotcome/dokaz/internal/branding"
	"github.com/preshotcome/dokaz/internal/drill"
)

// TestEnsureSampleTargetRematerializes reproduces the production bug where a
// sample drill failed with "stat sample.dump: no such file or directory" after
// a redeploy. The sample target row persists in the database, but the source
// directory is ephemeral on the deployed host, so the dump file vanishes on
// each deploy. ensureSampleTarget must rewrite the embedded dump every time —
// not only when first creating the target — so the runner's fetch step always
// finds the file.
func TestEnsureSampleTargetRematerializes(t *testing.T) {
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

	userID := uuid.New()
	accountID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id,email,password_hash) VALUES ($1,$2,'x')`,
		userID, "sample-"+userID.String()+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id,name,slug,plan) VALUES ($1,'s','s-'||$2,'trial')`,
		accountID, accountID.String()[:8]); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, accountID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})

	h := &Handlers{drills: drill.NewStore(pool), sourceDir: t.TempDir()}
	samplePath := filepath.Join(h.sourceDir, "_"+branding.Slug+"_sample", "sample.dump")

	// First run: creates the target and writes the dump.
	t1, err := h.ensureSampleTarget(ctx, accountID, userID)
	if err != nil {
		t.Fatalf("first ensureSampleTarget: %v", err)
	}
	if _, err := os.Stat(samplePath); err != nil {
		t.Fatalf("dump not written on first run: %v", err)
	}
	if t1.SourceURI != samplePath {
		t.Fatalf("target SourceURI = %q, want %q", t1.SourceURI, samplePath)
	}

	// Simulate a redeploy: the persistent target row stays, the ephemeral
	// file is wiped.
	if err := os.Remove(samplePath); err != nil {
		t.Fatalf("remove dump: %v", err)
	}

	// Second run: target already exists, but the file must be rewritten.
	t2, err := h.ensureSampleTarget(ctx, accountID, userID)
	if err != nil {
		t.Fatalf("second ensureSampleTarget: %v", err)
	}
	if t2.ID != t1.ID {
		t.Fatalf("expected same sample target, got %s then %s", t1.ID, t2.ID)
	}
	if _, err := os.Stat(samplePath); err != nil {
		t.Fatalf("dump NOT re-materialized after wipe — this is the prod bug: %v", err)
	}
	if fi, _ := os.Stat(samplePath); fi != nil && fi.Size() == 0 {
		t.Fatalf("re-materialized dump is empty")
	}
}
