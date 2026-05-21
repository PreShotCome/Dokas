package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func fixture(name string) string {
	return filepath.Join("..", "..", "testdata", "fixtures", name)
}

func TestDetectDumpFormat(t *testing.T) {
	cases := map[string]dumpFormat{
		"tiny.sql":  dumpPlainSQL,
		"tiny.dump": dumpArchive,   // pg_dump custom format (-Fc)
		"tiny.tar":  dumpArchive,   // pg_dump tar format (-Ft)
		"tiny-dir":  dumpDirectory, // pg_dump directory format (-Fd)
	}
	for name, want := range cases {
		got, err := detectDumpFormat(fixture(name))
		if err != nil {
			t.Fatalf("detectDumpFormat(%s): %v", name, err)
		}
		if got != want {
			t.Errorf("detectDumpFormat(%s) = %d, want %d", name, got, want)
		}
	}
}

// TestRestoreFormats provisions a sandbox per fixture format and confirms the
// dump restores and the seeded `events` table lands with all its rows —
// proving plain-SQL, custom, tar, and directory dumps are all supported.
func TestRestoreFormats(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	r := NewLocalRunner(dsn)

	for _, name := range []string{"tiny.sql", "tiny.dump", "tiny.tar", "tiny-dir"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			sb, err := r.Provision(ctx, uuid.New())
			if err != nil {
				t.Fatalf("provision: %v", err)
			}
			defer func() { _ = r.Teardown(ctx, sb) }()

			path, err := r.Fetch(ctx, sb, fixture(name))
			if err != nil {
				t.Fatalf("fetch: %v", err)
			}
			if err := r.Restore(ctx, sb, path); err != nil {
				t.Fatalf("restore: %v", err)
			}

			conn, err := pgx.Connect(ctx, sb.DSN)
			if err != nil {
				t.Fatalf("connect sandbox: %v", err)
			}
			defer conn.Close(ctx)
			var n int
			if err := conn.QueryRow(ctx, `SELECT count(*) FROM events`).Scan(&n); err != nil {
				t.Fatalf("count events: %v", err)
			}
			if n != 3 {
				t.Fatalf("events rows after restore = %d, want 3", n)
			}
		})
	}
}
