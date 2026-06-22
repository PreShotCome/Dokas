#!/usr/bin/env bash
# scripts/e2e.sh — one-command, zero-input end-to-end check.
#
# Boots a real Dokaz server against the local Postgres and runs the full
# verification stack against it:
#   1. go test ./...            (unit + Postgres-backed integration)
#   2. cmd/linkcheck            (every page renders; no broken links)
#   3. cmd/e2e-smoke            (signup -> sample drill -> signed PDF: PASSED)
#
# Designed to run unattended in a Claude Code web session, where the
# SessionStart hook (.claude/hooks/session-start.sh) has already started
# Postgres and exported DATABASE_URL. If DATABASE_URL is unset it falls back
# to the same CI-matching DSN the hook uses.
#
# Exit code is non-zero if any stage fails. The server is always torn down.
set -euo pipefail
cd "$(dirname "$0")/.."

PORT="${E2E_PORT:-8099}"
BASE_URL="http://127.0.0.1:${PORT}"
export DATABASE_URL="${DATABASE_URL:-postgres://restoredrill:restoredrill@127.0.0.1:5432/restoredrill?sslmode=disable}"

echo "==> DATABASE_URL=${DATABASE_URL%%\?*}?…"
echo "==> base URL ${BASE_URL}"

echo "==> migrate up"
go run ./cmd/migrate up

echo "==> go test ./..."
go test ./...

echo "==> building server"
SERVER_BIN="$(mktemp -d)/dokaz-server"
go build -o "$SERVER_BIN" ./cmd/server

EV_DIR="$(mktemp -d)"
SRC_DIR="$(mktemp -d)"
echo "==> booting server on ${PORT}"
ENV=dev ADDR=":${PORT}" BASE_URL="${BASE_URL}" \
  EVIDENCE_DIR="${EV_DIR}" SOURCE_DIR="${SRC_DIR}" \
  "$SERVER_BIN" >/tmp/e2e-server.log 2>&1 &
SERVER_PID=$!
cleanup() {
  kill "$SERVER_PID" 2>/dev/null || true
  wait "$SERVER_PID" 2>/dev/null || true
}
trap cleanup EXIT

# Wait for readiness.
for _ in $(seq 1 30); do
  curl -fsS -o /dev/null "${BASE_URL}/healthz" 2>/dev/null && break
  sleep 1
done
if ! curl -fsS -o /dev/null "${BASE_URL}/healthz" 2>/dev/null; then
  echo "!! server failed to come up; last log lines:" >&2
  tail -20 /tmp/e2e-server.log >&2
  exit 1
fi
echo "==> server up"

echo "==> linkcheck"
BASE_URL="${BASE_URL}" go run ./cmd/linkcheck

echo "==> e2e-smoke (signup -> drill -> PDF)"
BASE_URL="${BASE_URL}" go run ./cmd/e2e-smoke

echo ""
echo "  E2E GREEN — tests, links, and the full drill flow all pass."
