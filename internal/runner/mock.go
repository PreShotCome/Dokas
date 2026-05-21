package runner

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// LocalRunner executes drills against the same Postgres cluster that hosts
// the app database, using a per-drill temp database for isolation. It is the
// CI/dev runner: no Docker, no Fly Machines, just CREATE DATABASE.
//
// adminDSN must point at a Postgres role that can CREATE/DROP database. The
// app's own DATABASE_URL is fine when the role has the privilege (it does in
// local dev and CI).
type LocalRunner struct {
	adminDSN string
}

func NewLocalRunner(adminDSN string) *LocalRunner {
	return &LocalRunner{adminDSN: adminDSN}
}

// Provision creates an isolated database named drill_<short>. We connect with
// the admin DSN, run CREATE DATABASE, then hand back a DSN scoped to it.
func (r *LocalRunner) Provision(ctx context.Context, drillID uuid.UUID) (*Sandbox, error) {
	dbName := "drill_" + strings.ReplaceAll(drillID.String(), "-", "")[:16]

	if err := r.execAdmin(ctx, fmt.Sprintf(`CREATE DATABASE %q`, dbName)); err != nil {
		return nil, fmt.Errorf("create sandbox db: %w", err)
	}

	dsn, err := swapDatabase(r.adminDSN, dbName)
	if err != nil {
		// Best-effort cleanup so we don't orphan the DB.
		_ = r.execAdmin(ctx, fmt.Sprintf(`DROP DATABASE %q`, dbName))
		return nil, err
	}
	return &Sandbox{DrillID: drillID, DSN: dsn, Name: dbName}, nil
}

// Fetch for postgres_dump_local just validates the file exists and returns
// its absolute path. Later runners will copy from object storage.
func (r *LocalRunner) Fetch(ctx context.Context, _ *Sandbox, sourceURI string) (string, error) {
	abs, err := filepath.Abs(sourceURI)
	if err != nil {
		return "", fmt.Errorf("resolve source path: %w", err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("stat dump: %w", err)
	}
	if st.IsDir() {
		return "", fmt.Errorf("source is a directory, expected a file: %s", abs)
	}
	return abs, nil
}

// Restore applies the dump at localPath to the sandbox. Two formats supported:
//   - .sql (plain text) — applied with psql so script-style \i etc. work.
//   - .dump (pg_dump custom/-Fc) — applied with pg_restore.
//
// Anything else falls back to pg_restore, which sniffs the format itself.
func (r *LocalRunner) Restore(ctx context.Context, sb *Sandbox, localPath string) error {
	ext := strings.ToLower(filepath.Ext(localPath))

	switch ext {
	case ".sql":
		cmd := exec.CommandContext(ctx, "psql", "--quiet", "--no-psqlrc",
			"--set=ON_ERROR_STOP=1", "-d", sb.DSN, "-f", localPath)
		return runDumpCmd(cmd, "psql")
	default:
		cmd := exec.CommandContext(ctx, "pg_restore",
			"--no-owner", "--no-privileges",
			"--exit-on-error",
			"-d", sb.DSN, localPath)
		return runDumpCmd(cmd, "pg_restore")
	}
}

// Rehydrate reconstructs a Sandbox handle from a drill ID + bare DB name.
// Used by step workers when the in-process Sandbox struct from Provision is
// not available (e.g. the worker resumed after a crash and only knows the
// drill ID + the persisted sandbox_db column).
func (r *LocalRunner) Rehydrate(drillID uuid.UUID, dbName string) (*Sandbox, error) {
	dsn, err := swapDatabase(r.adminDSN, dbName)
	if err != nil {
		return nil, err
	}
	return &Sandbox{DrillID: drillID, Name: dbName, DSN: dsn}, nil
}

// Teardown drops the sandbox database. Safe to call on a nil/zero sandbox.
func (r *LocalRunner) Teardown(ctx context.Context, sb *Sandbox) error {
	if sb == nil || sb.Name == "" {
		return nil
	}
	// FORCE disconnects lingering sessions so DROP doesn't hang in CI.
	return r.execAdmin(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, sb.Name))
}

func (r *LocalRunner) execAdmin(ctx context.Context, sql string) error {
	conn, err := pgx.Connect(ctx, r.adminDSN)
	if err != nil {
		return fmt.Errorf("connect admin: %w", err)
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, sql)
	return err
}

func runDumpCmd(cmd *exec.Cmd, name string) error {
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Surface the first ~1KB of output; full restore logs can be huge.
		s := string(out)
		if len(s) > 1024 {
			s = s[:1024] + "...(truncated)"
		}
		return fmt.Errorf("%s failed: %w: %s", name, err, s)
	}
	return nil
}

// swapDatabase returns a copy of dsn with its database name replaced. It
// preserves the rest of the URL (auth, host, query params, etc).
func swapDatabase(dsn, dbName string) (string, error) {
	if dsn == "" {
		return "", errors.New("empty admin DSN")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse admin DSN: %w", err)
	}
	u.Path = "/" + dbName
	return u.String(), nil
}
