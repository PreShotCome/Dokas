# Dokaz

**Backup verification you can independently prove.** Dokaz periodically
restores your database dumps into an isolated sandbox, runs assertions
against the restored data, and produces evidence anyone can verify
without trusting us.

## Why you can trust the evidence

Most backup-verification tools hand you a green checkbox. Dokaz gives
you a four-link chain that a third party can walk end-to-end. Break any
link and verification fails loudly.

1. **Hashed input.** Before a drill restores anything, the dump file is
   streamed through SHA-256. The hex digest is stored on the drill row
   and rendered in the signed PDF. You re-hash the dump you hold (or
   that your auditor holds) with one shell command — `shasum -a 256
   dump.tar` — and prove it is byte-for-byte the file we drilled.
2. **In-sandbox restore + receipts.** The drill restores into a
   per-drill ephemeral Postgres on Fly Machines (or a temp DB on the
   local runner for dev). The signed PDF records every assertion's
   **kind, expected, actual, and pass/fail** — not just a verdict.
   `row_count`, `table_exists`, `column_exists`, `no_nulls`, and
   `sql_query` (write your own read-only SQL and capture the rows into
   evidence).
3. **Detached Ed25519 signature.** Every PDF is signed offline by the
   active Dokaz key. The signature attests `sha256(pdf) ‖
   signedAt(RFC3339Nano UTC)` — so a forgery has to match both the PDF
   bytes and the recorded timestamp, not one or the other. Past keys
   are kept as verification-only so evidence signed before a key
   rotation still verifies.
4. **Independently licensed verifier.** [`cmd/dokaz-verify`](cmd/dokaz-verify)
   is a single Go file that depends on `crypto/ed25519` and nothing of
   ours. It is licensed under **Apache-2.0** (see
   [`cmd/dokaz-verify/LICENSE`](cmd/dokaz-verify/LICENSE)) — separate from
   the rest of this repository — so you have an unambiguous right to build,
   run, and audit it yourself. Point it at the PDF, the signature JSON, and
   our published public key, and exit code 0 means the chain holds. Anyone
   with the three files can prove the result; the path does not go through
   Dokaz's servers, and there is no Dokaz SDK you are asked to trust.

```sh
# Independently verify a drill:
curl -H "Authorization: Bearer $KEY" https://app.dokaz.net/v1/drills/$ID/evidence  > drill.pdf
curl -H "Authorization: Bearer $KEY" https://app.dokaz.net/v1/drills/$ID/signature > sig.json
curl https://dokaz.net/.well-known/evidence-signing-keys.pem > dokaz.pem
go run ./cmd/dokaz-verify --pdf=drill.pdf --sig=sig.json --pubkey=dokaz.pem
# OK  key=9f2c4b…a17b  signed_at=2026-05-25T04:11:02Z  retain_until=2033-05-25T04:11:02Z
```

That's the differentiator. The rest of the README is the implementation
detail.

## What's in the repo

This repo contains the application (Go monolith) deployed at
`app.dokaz.net`. The marketing site lives in a separate repo. The
verifier CLI ships here so the chain stays in one auditable place.

## Status

All 11 rubric layers are built. Latest: hashed input + the
`dokaz-verify` CLI close the verifiability chain end-to-end.

Implemented:
- Chi + Templ + HTMX + Tailwind monolith
- Postgres sessions (Argon2id), audit log, security headers, signup/login
- River-backed drill orchestrator: `provision → fetch → restore → assert → report → teardown`
- LocalRunner sandbox: temp Postgres database per drill on the host cluster
- FlyMachineRunner: per-drill Fly Machine running an ephemeral Postgres
- Assertion kinds: `row_count`, `table_exists`, `column_exists`, `no_nulls`, `sql_query`
- SHA-256 hash of the dump bytes (input anchor of the evidence chain)
- Ed25519-signed evidence PDFs via `github.com/go-pdf/fpdf`
- `cmd/dokaz-verify` — stdlib-only third-party verifier
- Idempotency on `POST /drills` (per-account, per-key)
- Multi-tenant accounts + memberships; signup auto-creates a personal account
- RBAC (`owner`/`admin`/`member`/`viewer`/`exec`/`auditor`) via a single `Authorize` matrix
- Teams within an org — databases and members partition into teams; a member
  sees only their teams' databases (plus unassigned), owners/admins see all
- Email invitations (dev: link logged to stdout), account switcher
- Stripe billing — Checkout, Customer Portal, signed-webhook plan sync
- CSRF double-submit-cookie protection on every unsafe verb
- In-process token-bucket rate limiting (per-IP on auth, per-account elsewhere)
- Login brute-force throttle (lockout after repeated failures)
- HMAC-SHA256-signed webhooks with River-backed retry, delivery log, replay
- Per-account envelope encryption (AES-256-GCM, AAD-bound DEK)
- Evidence store abstraction (local filesystem; S3 Object Lock stubbed)
- Retention sweeper (River periodic job) — evidence/audit 7y, login attempts 30d
- GDPR/CCPA: JSON data export + account soft-delete → hard-delete (crypto-shred)
- Structured JSON request logs with trace_id / account_id correlation
- Prometheus metrics at `/metrics` (HTTP, drills, webhooks, queue depth)
- OpenTelemetry tracing — HTTP + drill-step spans (OTLP / stdout / noop)
- Error reporting via a Sentry seam (noop fallback); `/readyz` probe
- Incident runbook + Grafana dashboard + Prometheus alert rules
- Transactional email (Postmark seam; logs locally) — invitation + welcome
- Email suppression list fed by a Postmark bounce/complaint webhook
- Product analytics (PostHog seam) on signup / invite / drill events
- Feature flags (env-driven) — `self_serve_signup` gates the signup route
- TOTP MFA with replay protection + single-use recovery codes
- Magic-link sign-in with per-recipient throttling
- OAuth: Google + GitHub (PKCE, hardened against session-fixation)
- Staff admin panel — user lookup, safe (reason-logged) impersonation,
  drill replay, evidence regeneration
- Versioned `/v1` JSON API — API keys, `{data,meta,errors}` envelope,
  `Idempotency-Key` writes, cursor pagination, per-account rate limit,
  OpenAPI doc at `/openapi.json` + a `/docs` reference
- WCAG 2.2 AA pass — skip link, focus indicators, ARIA labels, landmarks
- Backup check-ins (heartbeats) — a public ping endpoint + overdue sweeper
  that alerts via webhooks when a backup job stops checking in

## Backup check-ins (heartbeats)

Drills *actively* restore-and-verify a dump; a heartbeat is the cheap, passive
complement — it confirms the backup job even ran. You register a monitor with
an expected period and a grace window, then have your backup script ping a
unique URL on success:

```sh
# at the end of your nightly backup cron
pg_dump … && curl -fsS https://app.dokaz.net/ping/<token>
```

If a check-in is overdue, a once-a-minute River sweeper flips the monitor to
**down**, fires the account's webhooks (`heartbeat.down`), and records an audit
event; the next successful ping flips it back **up** (`heartbeat.up`). Append
`/fail` to signal an explicit failure or `/start` to mark a long job's start.
The ping endpoint is unauthenticated by design — the token in the path is the
credential — CSRF-exempt and rate-limited per source IP.

## Local development

```sh
make dev
```

This starts Postgres in Docker, runs migrations (goose + River), fetches HTMX,
builds CSS, regenerates Templ files, ensures `tmp/evidence` exists, and
runs the server on `http://localhost:8080`.

To exercise a drill end-to-end:

1. Sign up at `/signup`.
2. From the dashboard, click **Connect a database**.
3. Use `testdata/fixtures/tiny.dump` as the source path, `events` as the
   assertion table, `1` as the minimum row count.
4. Go to `/drills`, pick the target, click **Run drill**, watch the steps
   tick through (HTMX polls every 2 s until terminal).
5. Download the PDF, fetch the signature JSON from `/v1/drills/{id}/signature`,
   and verify with `go run ./cmd/dokaz-verify`.

## Tests

```sh
DATABASE_URL=postgres://dokaz:dokaz@localhost:5432/dokaz?sslmode=disable \
  go test ./...
```

The drill integration test in `internal/drill/drill_integration_test.go`
needs `DATABASE_URL` to be set and `pg_restore` on `PATH`; otherwise it
skips.

## Layout

```
cmd/server               HTTP + River worker entrypoint
cmd/migrate              goose + River migration CLI
cmd/dokaz-verify       stdlib-only third-party evidence verifier (Apache-2.0)
internal/auth            sessions, password hashing, RBAC, MFA, magic-link
internal/apikey          /v1 API-key issuance + verification
internal/account         accounts, memberships, invitations, trial window
internal/billing         Stripe customer + Checkout + webhook (dedup, ordering)
internal/ratelimit       token-bucket limiter + middleware
internal/webhooks        signed webhook endpoints, delivery worker, dispatch
internal/evidence        evidence store + Ed25519 signing + AES-GCM cipher
internal/compliance      GDPR export, account purge, retention sweeper
internal/obs             logging, metrics, tracing, error reporting
internal/email           transactional email + suppression list
internal/analytics       product-event capture (PostHog seam)
internal/flags           feature-flag evaluation
internal/db              pgx pool, transaction helpers
internal/drill           drill domain (targets, drills, steps, results)
internal/drill/steps     River workers for each pipeline step
internal/runner          Runner interface + LocalRunner + FlyMachineRunner
internal/assertions      assertion kinds
internal/report          PDF rendering
internal/web             handlers + Templ templates
internal/web/csrf        CSRF double-submit middleware
internal/oauth           OAuth provider registry (Google, GitHub) with PKCE
migrations               goose SQL migrations
docs/runbooks            operational runbooks
dashboards               Grafana dashboard + Prometheus alert rules
testdata/fixtures        seeded pg_dump used by local dev + CI
assets                   Tailwind input, static files (HTMX, app.css, favicons)
branding                 logo SVGs + brand assets
```

See [`CHANGELOG.md`](CHANGELOG.md) for the record of shipped changes,
[`docs/plan.md`](docs/plan.md) for the full plan against the rubric,
[`docs/backlog.md`](docs/backlog.md) for deferred items, and
[`docs/security-audit-2026-05.md`](docs/security-audit-2026-05.md) for
the most recent third-party-style audit + fixes.

## License

Copyright (c) 2026 Ian Lee. All rights reserved. "Dokaz" and its logo are
trademarks of Ian Lee.

This repository is **proprietary and source-available**. It is published
for transparency, security review, and so customers and their auditors can
independently verify Dokaz evidence — **not** as open-source. No license to
use, copy, modify, or distribute the code is granted by its availability
here. See [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE) for the full terms.

**One exception:** the evidence verifier in
[`cmd/dokaz-verify/`](cmd/dokaz-verify) is licensed under **Apache-2.0**
([`cmd/dokaz-verify/LICENSE`](cmd/dokaz-verify/LICENSE)), so you may freely
build, run, modify, and distribute *that* tool to confirm evidence without
trusting our servers. Everything else is reserved.

For a commercial or evaluation license: legal@dokaz.net
