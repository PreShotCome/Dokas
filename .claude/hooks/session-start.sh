#!/bin/bash
# SessionStart hook for Claude Code on the web.
#
# Brings up Postgres and exports DATABASE_URL so the Postgres-backed
# integration tests (and `go run ./cmd/migrate`) run instead of skipping.
# Idempotent — safe to run on every session start.
set -euo pipefail

# Local dev machines have their own database setup; only act on the web.
if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  exit 0
fi

DSN="postgres://postgres:postgres@127.0.0.1:5432/restoredrill_test"

# Start the Postgres cluster if it is not already running.
pg_ctlcluster 16 main start >/dev/null 2>&1 || true
for _ in $(seq 1 30); do
  pg_isready -q && break
  sleep 1
done

# Give the postgres role a password (TCP auth needs one) and create the test
# database. Both steps are idempotent.
sudo -u postgres psql -tAc "ALTER ROLE postgres PASSWORD 'postgres'" >/dev/null
sudo -u postgres psql -tAc "SELECT 1 FROM pg_database WHERE datname='restoredrill_test'" \
  | grep -q 1 || sudo -u postgres createdb restoredrill_test

# Persist DATABASE_URL for the whole session.
echo "export DATABASE_URL=\"$DSN\"" >> "$CLAUDE_ENV_FILE"

# Warm the Go module cache and apply migrations so the schema is ready.
cd "$CLAUDE_PROJECT_DIR"
go mod download
DATABASE_URL="$DSN" go run ./cmd/migrate up

echo "session-start: Postgres up, DATABASE_URL exported, migrations applied"
