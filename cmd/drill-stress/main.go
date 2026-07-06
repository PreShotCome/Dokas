// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

// Command drill-stress drives the LocalRunner directly against synthetic
// pg_dumps of a configurable size, so we can see where the provision →
// fetch → restore → assert → teardown pipeline breaks before a paying
// customer discovers it. No HTTP, no signup, no queue — just the runner
// against a real Postgres, timed per step.
//
// Usage:
//
//	DATABASE_URL=postgres://... go run ./cmd/drill-stress -size 100MB
//
// Multiple sizes:
//
//	go run ./cmd/drill-stress -sizes 10MB,100MB,1GB
//
// Assertions run against the synthetic table so the assert step exercises
// a real query, not a no-op.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/preshotcome/dokaz/internal/assertions"
	"github.com/preshotcome/dokaz/internal/runner"

	"github.com/jackc/pgx/v5"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}

	sizes := flag.String("sizes", "10MB", "comma-separated dump sizes to test (e.g. 10MB,100MB,1GB)")
	keep := flag.Bool("keep", false, "keep the generated dumps + sandbox for inspection on failure")
	flag.Parse()

	work, err := os.MkdirTemp("", "drill-stress-*")
	if err != nil {
		log.Fatalf("mkdtemp: %v", err)
	}
	if !*keep {
		defer os.RemoveAll(work)
	} else {
		log.Printf("keeping working dir: %s", work)
	}

	r := runner.NewLocalRunner(dsn)

	for _, sizeStr := range strings.Split(*sizes, ",") {
		sizeStr = strings.TrimSpace(sizeStr)
		if sizeStr == "" {
			continue
		}
		bytes, err := parseSize(sizeStr)
		if err != nil {
			log.Fatalf("parse size %q: %v", sizeStr, err)
		}
		fmt.Printf("\n══════════════════════════════════════════════════════════\n")
		fmt.Printf("  drill-stress: target %s (%d bytes)\n", sizeStr, bytes)
		fmt.Printf("══════════════════════════════════════════════════════════\n")
		if err := runOne(work, dsn, r, sizeStr, bytes); err != nil {
			log.Printf("FAIL at %s: %v", sizeStr, err)
			os.Exit(1)
		}
	}
	fmt.Println("\nALL SIZES PASSED")
}

func runOne(work, dsn string, r runner.Runner, tag string, targetBytes int64) error {
	ctx := context.Background()
	drillID := uuid.New()

	// 1) Generate dump.
	dumpPath := filepath.Join(work, "dump-"+tag+".sql")
	genStart := time.Now()
	rowCount, err := generateDump(ctx, dsn, dumpPath, targetBytes)
	if err != nil {
		return fmt.Errorf("generate dump: %w", err)
	}
	stat, _ := os.Stat(dumpPath)
	fmt.Printf("  [generate] %d rows → %s dump in %s\n",
		rowCount, humanBytes(stat.Size()), time.Since(genStart).Round(10*time.Millisecond))

	// 2) Provision.
	provStart := time.Now()
	sb, err := r.Provision(ctx, drillID)
	if err != nil {
		return fmt.Errorf("provision: %w", err)
	}
	fmt.Printf("  [provision] %s in %s\n", sb.Name, time.Since(provStart).Round(10*time.Millisecond))
	defer func() {
		if err := r.Teardown(context.Background(), sb); err != nil {
			log.Printf("  [teardown] WARN: %v", err)
		} else {
			fmt.Printf("  [teardown] ok\n")
		}
	}()

	// 3) Fetch (LocalRunner just validates + hashes).
	fetchStart := time.Now()
	localPath, hash, err := r.Fetch(ctx, sb, dumpPath)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	fmt.Printf("  [fetch]    sha256=%s… in %s\n", hash[:16], time.Since(fetchStart).Round(10*time.Millisecond))

	// 4) Restore.
	restStart := time.Now()
	out, err := r.Restore(ctx, sb, localPath)
	restDur := time.Since(restStart)
	if err != nil {
		fmt.Printf("  [restore]  FAILED after %s\n", restDur.Round(10*time.Millisecond))
		fmt.Printf("  captured stdout+stderr:\n%s\n", string(out))
		return fmt.Errorf("restore: %w", err)
	}
	fmt.Printf("  [restore]  ok in %s (%.1f MB/s)\n",
		restDur.Round(10*time.Millisecond),
		float64(stat.Size())/(1024*1024)/restDur.Seconds())

	// 5) Assertions — row_count against the table we just seeded.
	assertStart := time.Now()
	conn, err := pgx.Connect(ctx, sb.DSN)
	if err != nil {
		return fmt.Errorf("sandbox connect: %w", err)
	}
	defer conn.Close(ctx)
	out1, err := assertions.Run(ctx, pgxQuerier{conn}, assertions.Spec{
		Kind:   "row_count",
		Config: map[string]any{"table": "stress_events", "expected": rowCount},
	})
	if err != nil {
		return fmt.Errorf("assert row_count: %w", err)
	}
	out2, err := assertions.Run(ctx, pgxQuerier{conn}, assertions.Spec{
		Kind:   "table_exists",
		Config: map[string]any{"table": "stress_events"},
	})
	if err != nil {
		return fmt.Errorf("assert table_exists: %w", err)
	}
	fmt.Printf("  [assert]   row_count(passed=%v) table_exists(passed=%v) in %s\n",
		out1.Passed, out2.Passed, time.Since(assertStart).Round(10*time.Millisecond))
	if !out1.Passed || !out2.Passed {
		return fmt.Errorf("assertions failed: row_count=%+v table_exists=%+v", out1, out2)
	}
	return nil
}

// generateDump creates a table stress_events with rows sized so the resulting
// pg_dump file (custom format, compressed) approaches targetBytes. Returns the
// row count actually written.
//
// Row shape: id BIGSERIAL, payload TEXT (~1 KB of random data), created_at
// TIMESTAMPTZ. Postgres compresses TEXT, so pg_dump custom is typically
// 30-50% of the raw table size — we oversize the row count and rely on the
// caller reading the actual file size.
func generateDump(ctx context.Context, appDSN, outPath string, targetBytes int64) (int, error) {
	// Each row is ~1 KB of high-entropy random data (bytea). zlib inside
	// pg_dump won't be able to shrink it, so 1 dump byte ≈ 1 payload byte.
	// Overhead (headers, per-row metadata) is small at scale.
	perRowDump := int64(1024)
	rowCount := int(targetBytes / perRowDump)
	if rowCount < 100 {
		rowCount = 100
	}

	seedTable := "public.stress_events_seed_" + strings.ReplaceAll(uuid.New().String()[:8], "-", "")
	dumpTable := "public.stress_events"

	conn, err := pgx.Connect(ctx, appDSN)
	if err != nil {
		return 0, err
	}
	defer conn.Close(ctx)

	// Drop old seed table if it's lingering from a prior run.
	if _, err := conn.Exec(ctx, "DROP TABLE IF EXISTS "+seedTable); err != nil {
		return 0, err
	}
	// Build the table with random data in one INSERT ... SELECT so we
	// don't marshal millions of rows through the pgx client. Use bytea
	// filled with gen_random_bytes so the payload is genuinely
	// incompressible — pg_dump's zlib pass won't shrink it, and the
	// dump file size roughly matches the target we asked for.
	create := fmt.Sprintf(`
		CREATE UNLOGGED TABLE %s AS
		SELECT
			g                                      AS id,
			gen_random_bytes(1024)                 AS payload,
			now() - (g * interval '1 second')      AS created_at
		FROM generate_series(1, %d) g;
	`, seedTable, rowCount)
	if _, err := conn.Exec(ctx, create); err != nil {
		return 0, err
	}
	defer conn.Exec(context.Background(), "DROP TABLE IF EXISTS "+seedTable) //nolint:errcheck

	// pg_dump wants a stable table name. Rename the seed to the shared
	// stress_events name for the dump, then rename back so parallel runs
	// don't stomp each other. Cleaner: dump the seed table under its own
	// name, and rewrite the schema on the sandbox side. Simpler for a
	// stress harness: dump-and-rename.
	if _, err := conn.Exec(ctx, "DROP TABLE IF EXISTS "+dumpTable); err != nil {
		return 0, err
	}
	if _, err := conn.Exec(ctx, "ALTER TABLE "+seedTable+" RENAME TO stress_events"); err != nil {
		return 0, err
	}
	defer conn.Exec(context.Background(), "DROP TABLE IF EXISTS "+dumpTable) //nolint:errcheck

	// Run pg_dump -Fc against the app DB, writing only stress_events.
	cmd := exec.CommandContext(ctx, "pg_dump", "-Fc", "-t", "stress_events", "-f", outPath, appDSN)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("pg_dump: %w: %s", err, stderr.String())
	}
	return rowCount, nil
}

// pgxQuerier adapts a *pgx.Conn for the assertions package. Same shape as
// internal/drill/steps but inlined so cmd/drill-stress can stand alone.
type pgxQuerier struct{ c *pgx.Conn }

func (q pgxQuerier) QueryRow(ctx context.Context, sql string, args ...any) interface {
	Scan(dest ...any) error
} {
	return q.c.QueryRow(ctx, sql, args...)
}

func (q pgxQuerier) Query(ctx context.Context, sql string, args ...any) (assertions.Rows, error) {
	rows, err := q.c.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgxRows{rows}, nil
}

type pgxRows struct{ pgx.Rows }

func (r pgxRows) FieldDescriptions() []assertions.FieldDescription {
	fds := r.Rows.FieldDescriptions()
	out := make([]assertions.FieldDescription, len(fds))
	for i, f := range fds {
		out[i] = assertions.FieldDescription{Name: string(f.Name)}
	}
	return out
}

// parseSize accepts "500KB", "10MB", "1GB" (base-2). Whitespace-tolerant.
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	var unit int64 = 1
	switch {
	case strings.HasSuffix(s, "KB"):
		unit, s = 1<<10, strings.TrimSuffix(s, "KB")
	case strings.HasSuffix(s, "MB"):
		unit, s = 1<<20, strings.TrimSuffix(s, "MB")
	case strings.HasSuffix(s, "GB"):
		unit, s = 1<<30, strings.TrimSuffix(s, "GB")
	case strings.HasSuffix(s, "B"):
		s = strings.TrimSuffix(s, "B")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, err
	}
	return n * unit, nil
}

func humanBytes(n int64) string {
	const kb = int64(1) << 10
	const mb = kb << 10
	const gb = mb << 10
	switch {
	case n >= gb:
		return fmt.Sprintf("%.2f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	default:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	}
}
