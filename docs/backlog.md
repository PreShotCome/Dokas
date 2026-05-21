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
- **Single assertion kind** — `deferred`. Only `row_count`. Plan calls for
  schema checks, FK integrity, table sizes, etc.
- **Dump format coverage** — `deferred`. Only plain `.sql` and `-Fc`
  `.dump` are exercised; the plan's fixture corpus (`-Fd`, base backups,
  pgBackRest, WAL-G) is not built.

## Layer 3 — Multi-tenant

- **Stripe billing is a skeleton** — `seam`. `billing.Customers` creates a
  Stripe customer only. No Checkout, subscriptions, metered usage, or plan
  enforcement. Plan tiers (`trial/starter/pro`) exist as a column but
  nothing reads them.
- **No ownership transfer** — `deferred`. Exactly one owner per account;
  the owner can't hand off. Invites can't grant `owner`.

## Layer 4 — Perimeter & webhooks

- **Webhook SSRF** — `debt`. The delivery worker does not block
  private/loopback target IPs. Noted in `runbooks/secret-rotation.md`.
  Needs an egress allowlist or IP filtering before untrusted customers.
- **No JSON API** — `deferred`. Webhooks are outbound only; there is no
  versioned `/v1/` REST API (plan layer 4). Idempotency is a form field,
  not the `Idempotency-Key` header.
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
- **Connected drill traces** — `debt`. Each drill step opens its own span;
  they are not stitched into one trace across River jobs (would need trace
  context propagated through job args).
- **`/metrics` is unauthenticated** — `debt`. Fine behind a network policy;
  should be locked down or moved to an internal port for public deploys.

## Layer 9 — Growth

- **Postmark / PostHog are seams** — `seam`. Without tokens the app uses
  `LogMailer` and `NoopAnalytics`.
- **PostHog flag backend** — `deferred`. `flags.Flags` only has the
  env-driven `StaticFlags`; no PostHog flag-evaluation impl.
- **A/B experiments, deliverability report** — `deferred`.
- **Email verification flow** — `deferred`. `users.email_verified` exists
  but is never set; no verification email/endpoint (layer 5 work).
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

- **CI `govulncheck`** — `debt`. Runs with `continue-on-error: true`;
  should block once the baseline is clean.
- **`goose validate` not in CI** — `deferred`. CI runs `migrate up` but
  doesn't validate migration pairing.
- **Down-migration prod safety** — `debt`. Down migrations are tested
  locally; the plan wants expand-then-contract verified on a prod-sized
  clone.
