# Restore Drill — Backup Verification SaaS

## Context

The user wants to build a real, production-grade app exercising all 11 layers of their fullstack rubric (foundation → support), not a portfolio toy. They asked for an "untapped but easy to access" avenue. Recommendation accepted: **Restore Drill as a Service** — a B2B SaaS that periodically verifies customer database backups by spinning up an ephemeral sandbox, restoring the latest dump, running assertions, and producing auditor-grade evidence (signed PDF + immutable log).

Why this product:
- Real, recurring pain ("untested backups don't exist"), validated by SOC 2 / HIPAA control requirements.
- Buyer is identifiable: seed/Series-A engineering leads at startups running managed Postgres who need audit evidence.
- Solo-dev tractable: backend-heavy, low-design-surface, boring proven tech.
- The 11-layer rubric maps directly to the product: ops, observability, compliance, and audit logs *are* the product.

Locked decisions (from clarifying questions):
- **GA surface**: Postgres only (defer MySQL/Mongo to v2).
- **Compliance posture**: SOC 2-ready by GA, HIPAA in v2.
- **Repo home**: new repo `preshotcome/restore-drill` (must be added to the GitHub MCP allowlist before push), branch `claude/plan-fullstack-architecture-DlqRq`.

## What this is NOT
- Not a backup *creator* (we verify the dumps the customer already produces).
- Not a real-time monitoring tool (drills are scheduled, not streamed).
- Not a multi-region HA platform — single region at GA.
- Not free / self-serve at GA (sales-led, $50–$500/mo per database).
- Not multi-engine at GA — Postgres only.
- Not HIPAA-certified at GA.

## Architecture

Two deployable artifacts, single Postgres:

1. **`app.restoredrill.io`** — Go monolith (Chi + Templ + HTMX + Tailwind) serving the authenticated dashboard, API, webhooks, and orchestrator. Single binary, deployed to Fly.io.
2. **`restoredrill.io`** (marketing/docs/blog) — Astro static site on Cloudflare Pages. Decoupled so MDX content + Lighthouse SEO don't fight HTMX.

Drill execution runs on **Fly Machines** spun on-demand per drill (no inbound network, scoped outbound to S3 + control plane, destroyed on completion). Job queue is **River** (Postgres-native) with each drill modeled as a multi-step workflow: `provision → fetch → restore → assert → report → teardown → bill`. Each step is its own River job with idempotency keys so failures resume mid-flight without re-restoring 80GB.

**Evidence bucket** is S3 (Object Lock in compliance mode, 7-year retention). **Working storage** (transient dumps mid-drill) is Cloudflare R2 (cheap egress). Reports are PDFs signed with a document-signing cert (DigiCert) and RFC 3161 timestamp — not self-signed.

## Stack

| Layer | Choice |
|---|---|
| Language | Go 1.22+ |
| Web | Chi, Templ, HTMX, Tailwind |
| Marketing | Astro, MDX, Cloudflare Pages |
| DB | Postgres (Neon, branching for migrations) |
| Migrations | `goose` (forward + down, tested against prod-sized clone) |
| Queue | River |
| Sandbox | Fly Machines API |
| Evidence | S3 + Object Lock |
| Working storage | Cloudflare R2 |
| Auth | Hand-rolled Postgres sessions (no Clerk lock-in), Argon2id, WebAuthn for MFA |
| Billing | Stripe (subscriptions + metered usage) |
| Email (txn) | Postmark |
| Email (broadcast) | Resend |
| Observability | OpenTelemetry → Grafana Cloud |
| Error tracking | Sentry |
| Flags + analytics | PostHog |
| Status page | instatus.com |
| CI/CD | GitHub Actions → Fly.io |
| IaC | Terraform |
| Support | Plain (shared inbox) |

## Plan against the 11 layers

### 1. Foundation
- Write the problem brief, ICP, and "what this is NOT" (above).
- ERD: `accounts`, `users`, `memberships`, `databases` (customer-registered), `drill_schedules`, `drills`, `drill_steps`, `assertions`, `assertion_results`, `evidence_blobs`, `audit_events`, `api_keys`, `webhooks`, `webhook_deliveries`, `invoices`. Soft delete on user-facing entities; hard delete on assertion results past retention window.

### 2. Product
- **Onboarding (Day 1 win)**: connect a Postgres backup source (S3 path + read-only IAM role), run a "first drill" within 60 seconds of signup, deliver a signed PDF before they leave the dashboard.
- **Core flows**: register database → configure drill schedule → view drill history → download evidence → invite team → connect webhook.
- **Every screen** ships happy / loading / empty / error / partial states. Pagination, sort, filter on the drill history table from day one.
- **Onboarding empty state** is the killer screenshot: "Your first drill runs in 3 minutes."

### 3. Data
- Migrations via `goose` with paired up/down, tested on a Neon branch seeded with 100k drills before merge.
- Indexes designed up-front for `drills(database_id, started_at DESC)`, `audit_events(account_id, at DESC)`, `assertion_results(drill_id)`.
- **Restore drills for our own DB**: weekly automated restore of our Postgres into a sandbox with integrity assertions. We eat our own dog food.
- Soft delete on accounts/users with a 30-day grace; hard delete + crypto-shred for GDPR/CCPA delete requests.
- Retention: drill evidence 7 years (Object Lock); raw working dumps purged on teardown (~minutes); audit logs 7 years.

### 4. API
- Versioned `/v1/`. JSON. Idempotency-Key header required for every state-changing endpoint, stored 24h.
- Pagination via opaque cursors. Standard `{data, meta, errors}` envelope.
- Rate limits: 60 req/min/account default, burst via token bucket. 429 with `Retry-After`.
- Webhooks: signed with HMAC-SHA256, exponential retry, replayable from dashboard, delivery log surfaced to customer.
- OpenAPI 3.1 spec generated from Go struct tags, hosted at `/docs`.

### 5. Auth & identity
- Email + password (Argon2id, 19MiB memory, 2 iterations).
- Magic link as alternative.
- WebAuthn / TOTP MFA, enforced for owners by default.
- Sessions in Postgres, opaque tokens in `__Host-` cookies, 14-day idle / 30-day absolute.
- Email verification, password reset with single-use tokens (15 min).
- Social login: Google + GitHub (covers ICP).
- **Account deletion endpoint live before any external signups**.
- RBAC: `owner`, `admin`, `member`, `viewer`; permissions checked in middleware against a single `Authorize(actor, action, resource)` call.

### 6. Security perimeter
- All input validated server-side with `go-playground/validator`. Never trust the client.
- Parameterized queries only (`sqlc`). No string SQL anywhere.
- HTMX responses set `Content-Type: text/html; charset=utf-8`; Templ auto-escapes.
- CSRF: double-submit cookie + per-request token on all unsafe verbs.
- CORS: locked to first-party origins only; API uses bearer tokens, no cookies cross-origin.
- Security headers: CSP (no `unsafe-inline`, strict-dynamic with nonces), HSTS preload, `Referrer-Policy: strict-origin-when-cross-origin`, `Permissions-Policy`, `X-Content-Type-Options`, `X-Frame-Options: DENY`.
- Secrets in Fly secrets / Doppler; rotation runbook quarterly; signing cert in a separate vault.
- Dependency scanning via `govulncheck` + Dependabot on PRs.
- **Audit log** is a first-class table (not log lines): actor, action, target, IP, UA, at; surfaced to customers, immutable, retained 7y.
- **Sandbox isolation** is the security keystone: no inbound, scoped outbound, fresh VM per drill, customer creds are short-lived assume-role tokens never persisted.

### 7. Operations
- **Environments**: `dev` (local docker compose), `staging` (Fly), `prod` (Fly). Identical IaC.
- **CI/CD**: GitHub Actions runs `golangci-lint`, `govulncheck`, `goose validate`, unit tests, integration tests (testcontainers), staging deploy on `main`, manual promote to prod with one-click rollback.
- **Deploy strategy**: blue/green via Fly's release command; database migrations expand-then-contract (never destructive in one release).
- **Rollback plan**: every release tagged, `fly deploy --image <prev>` documented in runbook; migrations down-tested.
- **IaC**: Terraform for Fly apps, Cloudflare, Postmark domain, S3 bucket + Object Lock policy, Stripe products.
- **Feature flags**: PostHog flags wrap every new surface.

### 8. Observability
- **Structured logs** (slog) → Grafana Loki via OTel. Every log line carries `trace_id`, `account_id`, `drill_id`.
- **Metrics** (Prometheus/OTel): drill success rate, p50/p95/p99 drill duration, queue depth, machine-provision latency, R2/S3 egress bytes.
- **Traces**: drill end-to-end span tree (provision → fetch → restore → assert → report → teardown).
- **Alerting**: PagerDuty (or just Telegram during solo phase) on: drill failure spike, queue depth > N, p95 drill > SLO, billing webhook failures, signing cert expiry < 30d.
- **Dashboards**: per-customer "your drills" view + internal SRE view.
- **Error tracking**: Sentry, source-mapped, with `account_id` tag.
- **Uptime + status page**: instatus.com, public components for `app`, `api`, `drill orchestrator`, `webhooks`.

### 9. Growth loops
- **Analytics**: PostHog events on signup, first-drill-completed, first-failure-caught, invite-sent.
- **A/B**: PostHog experiments, gated on flag.
- **Transactional email**: Postmark with SPF/DKIM/DMARC strict, bounce + complaint handling auto-suspends recipients, weekly deliverability report.
- **SEO**: Astro marketing site, MDX content, OpenGraph cards per page, `sitemap.xml`, `robots.txt`, JSON-LD Product schema.
- **Referral loop**: every signed PDF report has a footer "Verified by Restore Drill" linking to the marketing site (compliance-safe wording).

### 10. Compliance & legal
- ToS + Privacy Policy + DPA + Sub-processor list, drafted by counsel before first paid customer.
- Cookie consent only for analytics cookies (none on marketing if PostHog autocapture is off).
- **GDPR / CCPA**: data export endpoint (JSON of everything we hold) and delete endpoint, both wired before launch.
- **SOC 2 prep**: Vanta or Drata from month two; controls already implemented in layers 5/6/7/8.
- **Accessibility**: WCAG 2.2 AA targeted; keyboard nav, focus rings, ARIA on all custom widgets, axe-core in CI.
- No card data touches our servers (Stripe Checkout + Customer Portal only).
- Tax: Stripe Tax for global sales tax; US 1099-K handled by Stripe Connect if we ever do payouts (we don't at GA).

### 11. Support
- **Help docs** in Astro under `/docs`, MDX, with search via Pagefind.
- **In-app**: contextual help links, Plain widget for live chat.
- **Admin panel**: internal-only, behind staff SSO, supports user lookup, safe impersonation (logs an audit event with reason), refund, drill replay, evidence regen.
- **Incident response runbook** committed to repo: SEV definitions, comms templates, postmortem template, status page snippets.
- **On-call**: solo phase = me, with auto-paging only outside drill windows. Drills scheduled in customer business hours by default to avoid 3am pages.

## What we're explicitly punting

- MySQL, Mongo (v2).
- BYO-runner-in-customer-VPC (v2, but `Runner` interface designed for it day one).
- HIPAA BAA chain (v2 — when first healthcare customer commits).
- Mobile app (browser is enough).
- Real-time UI (HTMX polling on drill detail page is sufficient).
- Multi-region (single region at GA).

## Critical files (to be created in `preshotcome/restore-drill`)


```
/cmd/server/main.go                    # HTTP + River worker entrypoint
/cmd/migrate/main.go                   # goose runner
/internal/auth/                        # sessions, Argon2, WebAuthn, RBAC
/internal/api/v1/                      # versioned handlers, idempotency middleware
/internal/drill/orchestrator.go        # the multi-step workflow
/internal/drill/steps/                 # provision, fetch, restore, assert, report, teardown
/internal/runner/runner.go             # Runner interface (Fly Machines impl + future BYO)
/internal/runner/fly_machine.go
/internal/evidence/signer.go           # PDF signing + RFC 3161 timestamp
/internal/evidence/store.go            # S3 Object Lock writes
/internal/audit/log.go                 # immutable audit events
/internal/billing/stripe.go
/internal/email/postmark.go
/internal/webhooks/dispatch.go
/internal/web/                         # Templ components + HTMX handlers
/internal/web/components/              # state-machine components (happy/loading/empty/error/partial)
/migrations/                           # goose .sql files (paired up/down)
/terraform/                            # fly, cloudflare, postmark, s3
/runbooks/                             # incident, on-call, restore, cert-rotation
/.github/workflows/                    # ci.yml, deploy-staging.yml, deploy-prod.yml
```



Marketing repo (separate, `preshotcome/restore-drill-marketing`):

```
/src/pages/                            # Astro pages
/src/content/docs/                     # MDX docs
/src/content/blog/                     # MDX blog
```


## Implementation phasing (suggested order)

1. **Skeleton week**: Go monolith boot, Postgres + goose, session auth, Chi + Templ + HTMX + Tailwind. One end-to-end page (login → empty dashboard).
2. **First drill**: hardcoded Postgres source, Fly Machine provision, restore, basic row-count assertion, unsigned PDF. No auth perimeter yet on the worker — internal only.
3. **Multi-tenant**: accounts, memberships, RBAC, audit log, billing skeleton.
4. **Production perimeter**: CSP/CSRF/headers, rate limits, idempotency, webhook signing, secrets rotation runbook.
5. **Compliance polish**: Object Lock evidence bucket, real document-signing cert + RFC 3161, retention policies, data export + delete endpoints, ToS/PP/DPA.
6. **Ops & observability**: OTel, Grafana, Sentry, status page, alerts, dashboards, runbooks.
7. **Marketing site**: Astro on Cloudflare Pages, first 5 docs pages, SEO baseline, OG cards.
8. **Soft launch**: invite 5 design-partner customers, drill their real Postgres, iterate.

## Verification

End-to-end smoke test that proves the product, not just the code:

1. Sign up as a fresh user; receive verification email via Postmark; verify.
2. Connect a sandbox Postgres on Neon (read-only role on a backup bucket containing a real `pg_dump -Fc` of a ~1GB DB).
3. Trigger a manual drill from the dashboard.
4. Watch the drill detail page poll (HTMX) through `provision → fetch → restore → assert → report → teardown` with traces visible in Grafana.
5. Download the signed PDF; verify the document signature externally (e.g., Adobe Reader chain validation) and check the timestamp authority.
6. Confirm the evidence object is in S3 with Object Lock retention until-date populated.
7. Confirm audit log shows the drill, the download, and the IP/UA.
8. Trigger a deliberate failure: corrupt the dump in the source bucket; rerun; confirm the report flags the failure and the webhook fires with a valid HMAC.
9. Issue a GDPR data export for the account; verify JSON contents include every entity.
10. Issue an account deletion; verify soft-delete, then advance the deletion job and verify hard-delete + crypto-shred of evidence keys.
11. Force a deploy: push to `main`, observe staging deploy, promote to prod, then roll back. Migrations down-tested on Neon branch.

## Known risks (and what we'll do about them)

- **Dump format combinatorics** (the project killer per red-team): launch Postgres only, build a fixture corpus of dump variants (`-Fc`, `-Fd`, base backups, pgBackRest, WAL-G) before GA; CI runs the orchestrator against all fixtures.
- **Fly Machine cold-start + volume-attach latency on large dumps**: benchmark p95 drill on 50GB before pricing tiers are committed; if > 30 min, pre-warm a pool of machines per region.
- **Signing cert expiry / TSA outage**: alert 30 days before expiry; failover to a secondary TSA; refuse to issue unsigned reports.
- **Pager fatigue (solo on-call)**: schedule drills in customer-local business hours by default; no auto-page outside our own business hours unless customer pays for 24/7 tier.
- **Compliance chain gaps**: audit subprocessor BAA/DPA status before signing any HIPAA customer; gate HIPAA-tier features behind a flag until the chain is real.

---

## Phase 2 — First drill (orchestrator + mock Runner + first restore-and-assert)

### Goal
Stand up the end-to-end drill workflow so a logged-in user can click a button on the dashboard, watch a multi-step drill execute, and download an (unsigned) PDF report of the result. Real Postgres restore in an isolated sandbox; mock the cloud-compute layer so this runs locally without a Fly account.

### Architecture decisions (locked, do not relitigate)
- **Queue**: River (Postgres-native). Add it as a dependency, run its migrations.
- **Sandbox runner**: define a `Runner` interface in `internal/runner/`. Ship two implementations:
  - `mockRunner` — runs the restore in an ephemeral Docker container or a temp Postgres schema on the same host. Used for local dev + CI.
  - `flyMachineRunner` — stub the methods, return `ErrNotImplemented`. Real Fly Machines wiring is deferred to a later phase.
- **Drill workflow**: model each step as a separate River job with idempotency keys so a failure mid-flight resumes without redoing the prior step. Steps: `provision → fetch → restore → assert → report → teardown`.
- **Evidence**: write the PDF to local disk under `./tmp/evidence/<drill_id>.pdf` for now. Object Lock / S3 deferred to Phase 5.
- **Auth**: any logged-in user can trigger their own drill. Multi-tenant accounts come in Phase 3.

### Data model additions (new goose migration)
- `database_targets` — id, user_id, name, source_kind ('postgres_dump_local' for now), source_uri, created_at, deleted_at
- `drills` — id, target_id, status (pending/running/succeeded/failed), started_at, completed_at, error, evidence_path
- `drill_steps` — id, drill_id, name (provision/fetch/restore/assert/report/teardown), status, started_at, completed_at, error, idempotency_key
- `assertion_results` — id, drill_id, kind ('row_count'), expected JSONB, actual JSONB, passed BOOL
- Index `drills(target_id, started_at DESC)`, `drill_steps(drill_id)`.

### Concrete deliverables
1. Add River + run its migrations from the migrate CLI.
2. `internal/runner/runner.go` with the interface + `internal/runner/mock.go` + `internal/runner/fly_machine.go` (stub).
3. `internal/drill/orchestrator.go` enqueues the seven River jobs for a new drill.
4. `internal/drill/steps/` — one file per step, each is a River JobArgs+Worker. Idempotency-keyed.
5. `internal/assertions/row_count.go` — runs `SELECT COUNT(*) FROM <table>` in the sandbox, compares to expected.
6. `internal/report/pdf.go` — write a minimal unsigned PDF (use `github.com/jung-kurt/gofpdf` or `github.com/go-pdf/fpdf`) summarizing the drill: timestamps per step, assertions table, pass/fail.
7. Web routes (under `RequireUser`):
   - `GET  /databases/new` + `POST /databases` — register a target (just name + a local file path for the dump in this phase)
   - `GET  /drills` — list user's drills (table with status, started_at, duration; happy/loading/empty/error states)
   - `POST /drills` — kick off a drill for a target (idempotency-key required)
   - `GET  /drills/{id}` — drill detail with HTMX polling on the step list (`hx-get="/drills/{id}/steps" hx-trigger="every 2s"` until terminal)
   - `GET  /drills/{id}/evidence` — download the PDF (auth-gated; log to audit_events)
8. Audit events for `drill.created`, `drill.completed`, `drill.failed`, `evidence.downloaded`.
9. Test fixture: a tiny seeded Postgres dump under `testdata/fixtures/tiny.dump` (a few rows of an `events` table) so local dev + CI can exercise the full pipeline.
10. Update CI to run an integration test that triggers a drill and asserts the PDF appears on disk.

### Out of scope (do NOT build these)
- Multi-tenant accounts / teams / RBAC (Phase 3)
- Stripe billing (Phase 3)
- Real Fly Machines provisioning (later phase)
- S3 Object Lock / signed PDFs / RFC 3161 (Phase 5)
- Webhooks (Phase 4)
- Marketing site, docs (Phase 7)
- MySQL / Mongo support

### Verification
Locally:
1. `make dev` boots Postgres + server.
2. Sign up, log in.
3. Register a database target pointing at the seeded fixture.
4. Click "Run drill", watch the steps tick through to completed.
5. Download the PDF, open it, confirm it shows step timings and a passed row-count assertion.
6. `psql` into the dev DB: confirm `audit_events` has `drill.created`, `drill.completed`, `evidence.downloaded` rows.
7. `go test ./...` passes including the new integration test.

### When you're done
Commit on the existing Phase 2 branch (`claude/restore-drill-phase-2-vHjzy`). One logical commit per concern is fine; squash if it gets messy. Push and stop — do not open a PR.
