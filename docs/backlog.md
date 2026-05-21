# Backlog — deferred items & known limitations

A running log of everything consciously deferred, stubbed, or left as a
known limitation across the phases so far. Each item says **what**, **why**,
and **where it should land**. Kept current as phases land.

Status key: `seam` = interface exists, real impl deferred · `deferred` =
not started, planned · `debt` = works but should be revisited.

## Layer 2 — First drill

- **Fly Machines sandbox runner** — `seam`. `runner.FlyMachineRunner`
  returns `ErrNotImplemented`; drills run on `LocalRunner` (temp Postgres DB
  on the host). Real per-drill cloud sandboxes are a later phase.
- **Dump format coverage** — `deferred`. Only plain `.sql` and `-Fc`
  `.dump` are exercised; the plan's fixture corpus (`-Fd`, base backups,
  pgBackRest, WAL-G) is not built.

## Layer 3 — Multi-tenant

- **Stripe billing is a skeleton** — `seam`. `billing.Customers` creates a
  Stripe customer only. No Checkout, subscriptions, metered usage, or plan
  enforcement. Plan tiers (`trial/starter/pro`) exist as a column but
  nothing reads them.
## Layer 4 — Perimeter & webhooks

- **MFA / magic links / social login** — `deferred`. Plan layer 5 identity
  work; not built. Password auth only.

## Layer 5 — Compliance / evidence

- **Document-signing cert** — `seam`. Evidence is signed with an Ed25519
  key (ephemeral in dev). The plan wants a real DigiCert document-signing
  cert + chain. `EVIDENCE_SIGNING_KEY` swaps in a persistent key; a full
  cert chain is deferred.
- **RFC 3161 timestamp** — `seam`. The signature covers `signed_at`, a
  self-asserted timestamp. A real RFC 3161 TSA (ASN.1 token) is deferred.
- **S3 Object Lock** — `seam`. `evidence.S3Store` is a stub; evidence lives
  on local disk. Retention is enforced in the app layer, not by Object
  Lock.
- **Crypto-shred** — `debt`. Evidence is not encrypted at rest, so
  "crypto-shred" on account deletion is plain file deletion. True
  crypto-shred needs at-rest encryption with a per-account key.
- **Signing-key rotation** — `debt`. Evidence signed with an old key fails
  verification after rotation; there is no multi-key verification set.
- **Legal copy** — `deferred`. ToS/Privacy/DPA pages are DRAFT placeholders
  pending counsel.

## Layer 6 — Observability

- **No real backends** — `seam`. OTLP collector, Grafana, and Sentry are
  config-gated; locally tracing uses the stdout exporter and errors use the
  noop reporter. Dashboards/alerts are committed as IaC, not deployed.

## Layer 9 — Growth

- **Postmark / PostHog are seams** — `seam`. Without tokens the app uses
  `LogMailer` and `NoopAnalytics`.
- **PostHog flag backend** — `deferred`. `flags.Flags` only has the
  env-driven `StaticFlags`; no PostHog flag-evaluation impl.
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
