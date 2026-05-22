# Backlog — deferred items & known limitations

A running log of everything consciously deferred, stubbed, or left as a
known limitation across the phases so far. Each item says **what**, **why**,
and **where it should land**. Kept current as phases land.

Status key: `seam` = interface exists, real impl deferred · `deferred` =
not started, planned · `debt` = works but should be revisited.

## Layer 2 — First drill

- **Physical-backup formats** — `deferred`. All four pg_dump *logical*
  formats are supported (see Resolved); physical backups — base backups,
  pgBackRest, WAL-G — need whole-cluster restore and are not built.

## Layer 3 — Multi-tenant

- **Plan enforcement & metered billing** — `deferred`. The Stripe
  subscription lifecycle is built (Checkout, Customer Portal, webhook sync —
  see Resolved), so `accounts.plan` is accurate. But no feature is gated on
  the tier yet, and there is no metered/usage billing.
## Layer 4 — Perimeter & webhooks


## Layer 5 — Compliance / evidence

- **Document-signing cert** — `seam`. Evidence is signed with an Ed25519
  key (ephemeral in dev). The plan wants a real DigiCert document-signing
  cert + chain. `EVIDENCE_SIGNING_KEY` swaps in a persistent key; a full
  cert chain is deferred. Deliberately not coded — unverified
  security-critical crypto (CMS/PKCS#7 + KMS/HSM key custody) would
  undermine the feature it supports. See `docs/runbooks/signing-cert.md`
  for the seam, the interim, and a bounded implementation plan.
- **RFC 3161 timestamp** — `seam`. The signature covers `signed_at`, a
  self-asserted timestamp bound into the Ed25519 signature. A real RFC 3161
  TSA (DER-encoded ASN.1 token) is deferred — for the same reason as the
  signing cert, and built together with it. See
  `docs/runbooks/rfc3161.md`.
- **Legal copy** — `deferred`. ToS/Privacy/DPA pages are DRAFT placeholders
  pending counsel.

## Layer 9 — Growth

- **Postmark / PostHog are seams** — `seam`. Without tokens the app uses
  `LogMailer` and `NoopAnalytics`.
- **A/B experiments, deliverability report** — `deferred`.
- **Marketing site** — `deferred`. The Astro site + its SEO (OG cards,
  JSON-LD, sitemap, MDX content) is Phase 7 in a separate repo.

## Layer 11 — Support

- **Staff SSO** — `debt`. Staff are flagged via `users.is_staff`, promoted
  from the `STAFF_EMAILS` allowlist at signup. The plan wants real staff
  SSO behind the admin panel.
- **Plain live-chat widget** — `deferred`. Third-party chat JS would
  violate the CSP (`script-src 'self'`); in-app help is a static `/help`
  page for now. The widget belongs on the marketing site or behind a CSP
  carve-out.
- **Help docs** — `deferred`. The full docs site (Astro + MDX + Pagefind
  search) is Phase 7, a separate repo. `/help` is an interim FAQ.
- **Admin refunds** — `deferred`. The plan's admin panel includes refunds;
  billing is still a skeleton, so there is nothing to refund yet.

## Cross-cutting

- **Down-migration prod safety** — `debt`. Down migrations are tested
  locally and CI checks every migration declares an Up + Down; the plan
  wants expand-then-contract verified on a prod-sized clone.

## Resolved

Layer-2 sandbox:

- **Fly Machines runner** — `runner.FlyMachineRunner` provisions a dedicated
  ephemeral Fly Machine (a per-drill Postgres VM) instead of a temp database
  on the app's host. `internal/fly` is a unit-tested Machines API client.
  Build-verified only — not exercised against the live Fly API. Activated by
  `FLY_API_TOKEN` + `FLY_APP_NAME`; see `docs/runbooks/fly.md`.

Layer-5 evidence storage:

- **S3 / R2 evidence store** — `evidence.S3Store` stores evidence in an
  S3-compatible bucket (AWS S3 or Cloudflare R2), signing requests with
  hand-rolled AWS SigV4 (no SDK; the signer is unit-tested against AWS's
  published vector). Activated by `EVIDENCE_S3_*` env vars; with Object Lock
  the bucket enforces immutability. See `docs/runbooks/s3.md`.

Layer-6 observability:

- **OpenTelemetry + Sentry backends** — the integration code was already in
  `internal/obs` (OTLP trace exporter, Prometheus metrics, structured logs,
  Sentry SDK). Documented activation: `OTEL_TRACES_EXPORTER`/`OTEL_EXPORTER_*`
  point traces at a collector; `SENTRY_DSN` enables error reporting. See
  `docs/runbooks/observability.md` and `sentry.md`. Grafana dashboards as IaC
  remain deferred.

Layer-9 growth:

- **PostHog feature-flag backend** — `flags.PostHogFlags` evaluates flags via
  PostHog's `/decide` endpoint with a 60s per-distinct-ID cache and a
  background refresh, so flag checks never block a request. Falls back to the
  static env defaults on any miss/error. Activated by `POSTHOG_API_KEY`.

Layer-3 billing:

- **Stripe subscription lifecycle** — `internal/billing` talks to Stripe's
  REST API directly: customer creation, subscription Checkout, the Customer
  Portal, and a signature-verified `/webhooks/stripe` endpoint that syncs
  `customer.subscription.*` events to the account's plan + status. Activated
  by `STRIPE_*` env vars; see `docs/runbooks/stripe.md`.

Layer-2 drills:

- **pg_dump format coverage** — the runner restores all four pg_dump
  logical formats: plain SQL (`-Fp` → psql), custom (`-Fc`), tar (`-Ft`),
  and directory (`-Fd`) archives (→ pg_restore). The format is detected
  from file content (PGDMP / ustar magic) or directory structure, not the
  extension. A fixture corpus (`tiny.sql/.dump/.tar/-dir`) exercises each
  via an integration test.

Layer-5 compliance:

- **Crypto-shred** — evidence PDFs are now encrypted at rest with per-account
  envelope encryption: a server master key (`EVIDENCE_ENCRYPTION_KEY`) wraps
  each account's data-encryption key, stored in `account_evidence_keys`; the
  DEK encrypts the PDF (AES-256-GCM). A GDPR hard delete destroys the wrapped
  DEK, so every evidence file for the account becomes permanently
  undecryptable — a true crypto-shred — even if a file copy survives.

Bug-report remediation pass (correctness + security audit):

- Drill pipeline: transient store/lookup errors now retry via River instead
  of permanently failing a drill; the `Runner` interface gained `Rehydrate`
  so non-local runners work; `Restore` sniffs the dump's magic header rather
  than trusting the file extension; `MarkStepSucceeded` can't resurrect a
  skipped step; the evidence path is recorded before the report step is
  marked done.
- `/v1` idempotency no longer caches 5xx responses (a transient failure is
  retryable, not replayed forever).
- TOTP codes are single-use (replay-protected via a stored counter); the
  session token is rotated when MFA completes.
- Last-owner demotion/removal is transactional + row-locked (no ownerless
  account race); invitations can only be accepted by the invited email;
  API keys on a soft-deleted account stop authenticating.
- `accountSwitch` redirect target goes through `safeNext` (no open
  redirect); a global request-body cap blocks oversized-POST DoS.
- Retention sweep continues past a failing step; the GDPR export is
  buffered so a mid-stream failure returns a clean 500; evidence PDF tables
  wrap text and paginate.
- Webhook fan-out is idempotent (per-event delivery dedup + by-args job
  uniqueness); the rate limiter guards against a zero rate.

Layer-5 identity:

- **Magic-link login** — passwordless sign-in: `/login/magic` emails a
  one-time link (`magic_link_tokens`, hashed at rest, 15-minute TTL) and
  `GET /login/magic/{token}` consumes it to start a session. The request
  response is identical for registered and unregistered emails (no account
  enumeration); MFA, when on, still applies — the link replaces the
  password, not the second factor. Expired tokens are pruned by the
  retention sweeper.
- **TOTP MFA** — RFC 6238 two-factor auth, implemented in-repo (no external
  dependency). Users enrol from `/account/mfa` (authenticator-app secret +
  confirmation code) and get ten single-use recovery codes. Login is a two
  step flow: a correct password creates an `mfa_pending` session that does
  not authenticate app requests until `/login/mfa` verifies a TOTP or
  recovery code.

Layer-5 evidence:

- **Signing-key rotation** — the evidence signer now keeps a verification
  key *set*: it signs with one active key but verifies against the active
  key plus any retired keys supplied via `EVIDENCE_VERIFICATION_KEYS`
  (concatenated PEM public keys). `Verify` resolves the verifying key by
  fingerprint, so evidence signed before a rotation still verifies.

Layer-9 growth:

- **Email verification flow** — signup issues a one-time verification token
  (`email_verification_tokens`, hashed at rest, 24h TTL) and emails the
  link. `GET /verify-email/{token}` consumes it and sets
  `users.email_verified`; unverified users see a dismissable-by-verifying
  banner with a resend action. Expired tokens are pruned by the retention
  sweeper. Verification is a soft nudge — it does not gate app access.

Layer-2 assertions:

- **Multiple assertion kinds** — assertions moved off the two baked-in
  `database_targets` columns into their own table; a target now carries any
  number of typed checks (`row_count`, `table_exists`, `column_exists`,
  `no_nulls`). The assert step dials the restored sandbox directly and runs
  each, recording one `assertion_results` row per check. Managed from a new
  `/databases/{id}` detail page and surfaced as an `assertions` array on the
  `/v1` database endpoints.

Tech-debt burndown pass:

- **Webhook SSRF** — the delivery worker's HTTP client now refuses to
  connect to private / loopback / link-local addresses (production only;
  dev keeps localhost webhooks working).
- **`/metrics` auth** — gated behind `METRICS_TOKEN` (bearer) when set.
- **Connected drill traces** — trace context is propagated through River
  job metadata; a drill's six step spans now form one trace tree.
- **Ownership transfer** — an owner can hand off the owner role to a
  member; the old owner becomes admin, atomically.
- **CI `govulncheck`** — now blocking; the Go toolchain was bumped to
  1.25.10 to clear the stdlib findings.
- **CI migration check** — CI verifies every migration declares both a
  `+goose Up` and `+goose Down` section.

Layer-4 API:

- **`/v1` JSON API** — versioned REST API: API-key auth, the
  `{data,meta,errors}` envelope, `Idempotency-Key`-gated writes, opaque
  cursor pagination, a per-account 60/min rate limit, and an OpenAPI 3.1
  document at `/openapi.json` with a `/docs` reference page.
- **API key scopes** — keys carry a scope set (`databases:read`,
  `databases:write`, `drills:read`, `drills:write`); the `/v1` router gates
  each endpoint on the scope it needs and returns `403 insufficient_scope`
  otherwise. Scopes are chosen with checkboxes on key creation — untick the
  write scopes for a read-only key.
