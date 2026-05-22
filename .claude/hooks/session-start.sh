#!/bin/bash
# SessionStart hook for Claude Code on the web.
#
# Brings up Postgres and exports DATABASE_URL so the Postgres-backed
# integration tests (and `go run ./cmd/migrate`) run instead of skipping.
#
# The role, database name, and DSN deliberately match .github/workflows/ci.yml
# (POSTGRES_USER/DB = restoredrill) so local test runs reproduce CI exactly —
# e.g. role-specific failures are caught here, not after a push.
#
# Idempotent — safe to run on every session start.
set -euo pipefail

# Local dev machines have their own database setup; only act on the web.
if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  exit 0
fi

DSN="postgres://restoredrill:restoredrill@127.0.0.1:5432/restoredrill?sslmode=disable"

# Start the Postgres cluster if it is not already running.
pg_ctlcluster 16 main start >/dev/null 2>&1 || true
for _ in $(seq 1 30); do
  pg_isready -q && break
  sleep 1
done

# Create the CI-matching role (LOGIN SUPERUSER — the drill runner needs
# CREATE DATABASE) and database. Both steps are idempotent.
sudo -u postgres psql -tAc \
  "SELECT 1 FROM pg_roles WHERE rolname='restoredrill'" | grep -q 1 \
  || sudo -u postgres psql -tAc \
       "CREATE ROLE restoredrill LOGIN SUPERUSER PASSWORD 'restoredrill'"
sudo -u postgres psql -tAc \
  "SELECT 1 FROM pg_database WHERE datname='restoredrill'" | grep -q 1 \
  || sudo -u postgres createdb -O restoredrill restoredrill

# Persist DATABASE_URL for the whole session.
echo "export DATABASE_URL=\"$DSN\"" >> "$CLAUDE_ENV_FILE"

# Warm the Go module cache and apply migrations so the schema is ready.
cd "$CLAUDE_PROJECT_DIR"
go mod download
DATABASE_URL="$DSN" go run ./cmd/migrate up

echo "session-start: Postgres up (role 'restoredrill', matching CI), migrations applied"
