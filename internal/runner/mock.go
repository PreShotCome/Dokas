package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
//
// The sandbox name is deterministic per drill, so Provision is idempotent: a
// retry (after a transient failure recording the name) drops any half-created
// leftover first, rather than failing on "database already exists".
func (r *LocalRunner) Provision(ctx context.Context, drillID uuid.UUID) (*Sandbox, error) {
	dbName := "drill_" + strings.ReplaceAll(drillID.String(), "-", "")[:16]

	if err := r.execAdmin(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, dbName)); err != nil {
		return nil, fmt.Errorf("drop stale sandbox db: %w", err)
	}
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

// Fetch for postgres_dump_local validates the dump source and returns
// its absolute path AND a SHA-256 hash of its contents.
func (r *LocalRunner) Fetch(_ context.Context, _ *Sandbox, sourceURI string) (string, string, error) {
	return fetchDump(sourceURI)
}

// fetchDump resolves and validates a dump source path, then streams it
// through SHA-256 to produce the hash that anchors the evidence chain.
// A plain file is hashed directly; a pg_dump -Fd directory is hashed
// over the concatenation of its files in sorted name order so the same
// dump always produces the same hash.
func fetchDump(sourceURI string) (string, string, error) {
	abs, err := filepath.Abs(sourceURI)
	if err != nil {
		return "", "", fmt.Errorf("resolve source path: %w", err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", "", fmt.Errorf("stat dump: %w", err)
	}
	if st.IsDir() {
		if _, err := os.Stat(filepath.Join(abs, "toc.dat")); err != nil {
			return "", "", fmt.Errorf("source directory is not a pg_dump -Fd archive (no toc.dat): %s", abs)
		}
	}
	hash, err := hashDump(abs)
	if err != nil {
		return "", "", fmt.Errorf("hash dump: %w", err)
	}
	return abs, hash, nil
}

// hashDump returns the hex SHA-256 of a dump. For a plain file the hash
// is over the file's bytes. For a pg_dump -Fd directory the hash is
// over the concatenation of regular-file contents in sorted-name order
// — stable across copies and operating systems.
func hashDump(absPath string) (string, error) {
	st, err := os.Stat(absPath)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if !st.IsDir() {
		f, err := os.Open(absPath)
		if err != nil {
			return "", err
		}
		defer f.Close()
		if _, err := io.Copy(h, f); err != nil {
			return "", err
		}
		return hex.EncodeToString(h.Sum(nil)), nil
	}
	// Directory: hash each regular file's bytes in sorted name order.
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Type().IsRegular() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		f, err := os.Open(filepath.Join(absPath, name))
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(h, f); err != nil {
			_ = f.Close()
			return "", err
		}
		_ = f.Close()
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// dumpFormat is how a fetched dump should be restored.
type dumpFormat int

const (
	dumpPlainSQL  dumpFormat = iota // plain-text SQL script — psql -f
	dumpArchive                     // custom (-Fc) or tar (-Ft) archive — pg_restore
	dumpDirectory                   // directory (-Fd) archive — pg_restore
)

// Restore applies the dump at localPath to the sandbox. The format is
// detected from the file's content (or, for a directory, its structure), not
// its extension, so a misnamed dump is still restored with the right tool —
// rather than failing, or worse, restoring partially while looking fine:
//   - plain SQL    — applied with psql
//   - custom / tar — applied with pg_restore (it auto-detects either)
//   - directory    — applied with pg_restore reading the directory
func (r *LocalRunner) Restore(ctx context.Context, sb *Sandbox, localPath string) ([]byte, error) {
	return restoreDump(ctx, sb.DSN, localPath)
}

// restoreDump loads a dump into the database at dsn, picking psql or
// pg_restore from the detected format. Returns the combined output
// (stdout+stderr) of the restore subprocess for evidence capture.
func restoreDump(ctx context.Context, dsn, localPath string) ([]byte, error) {
	format, err := detectDumpFormat(localPath)
	if err != nil {
		return nil, err
	}
	// Preflight: the restore shells out to a PostgreSQL client binary. A
	// missing binary is an operator-environment problem, not a bad dump, so
	// surface an actionable message instead of a raw "executable not found".
	tool := "pg_restore"
	if format == dumpPlainSQL {
		tool = "psql"
	}
	if _, err := exec.LookPath(tool); err != nil {
		return nil, fmt.Errorf("%s is required to restore this dump but was not "+
			"found on PATH — install the PostgreSQL client tools (which "+
			"provide psql and pg_restore) and ensure their bin directory is "+
			"on PATH", tool)
	}
	if format == dumpPlainSQL {
		cmd := exec.CommandContext(ctx, "psql", "--quiet", "--no-psqlrc",
			"--set=ON_ERROR_STOP=1", "-d", dsn, "-f", localPath)
		return runDumpCmd(cmd, "psql")
	}
	cmd := exec.CommandContext(ctx, "pg_restore",
		"--no-owner", "--no-privileges", "--exit-on-error",
		"-d", dsn, localPath)
	return runDumpCmd(cmd, "pg_restore")
}

// detectDumpFormat classifies a fetched dump by its content or structure.
func detectDumpFormat(path string) (dumpFormat, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("stat dump: %w", err)
	}
	if info.IsDir() {
		return dumpDirectory, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open dump: %w", err)
	}
	defer f.Close()
	head := make([]byte, 512)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return 0, fmt.Errorf("read dump header: %w", err)
	}
	head = head[:n]
	// Custom-format archive: 5-byte "PGDMP" magic at the start.
	if len(head) >= 5 && string(head[:5]) == "PGDMP" {
		return dumpArchive, nil
	}
	// Tar-format archive: POSIX "ustar" magic at offset 257.
	if len(head) >= 262 && string(head[257:262]) == "ustar" {
		return dumpArchive, nil
	}
	return dumpPlainSQL, nil
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

func runDumpCmd(cmd *exec.Cmd, name string) ([]byte, error) {
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Surface the first ~1KB of output; full restore logs can be huge.
		s := string(out)
		if len(s) > 1024 {
			s = s[:1024] + "...(truncated)"
		}
		return out, fmt.Errorf("%s failed: %w: %s", name, err, s)
	}
	return out, nil
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
