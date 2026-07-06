# Changelog

All notable, user- and auditor-facing changes to Dokaz are recorded here.
The format follows [Keep a Changelog](https://keepachangelog.com/); dates are
UTC. Full commit-level history lives in git; deferred items and known
limitations live in [`docs/backlog.md`](docs/backlog.md).

## [2026-07-06]

### Added
- **Teams within an organization** — an account can partition its databases
  and members into teams. A non-privileged member sees only their teams'
  databases (plus any left unassigned); owners and admins see everything.
  Managed from a new **Teams** page. (#29)
- **Evidence master-key rotation** — the at-rest encryption master key can now
  be rotated with zero data loss: retired keys are accepted during a rotation
  and each per-account key is re-wrapped under the new key on access. Ships an
  `evidence-keys` ops tool (`verify` / `rewrap` / `shred-poison`) and a
  rotation runbook (`docs/runbooks/evidence-key-rotation.md`).
- **"Verify a report" in the main navigation** — the public evidence verifier
  is now reachable from the site header nav and the in-app sidebar, not only
  the footer.
- **Offline-verifier discoverability** — the `/verify` page now links to the
  Apache-2.0 `dokaz-verify` CLI source and shows the command to run it, so an
  auditor can verify evidence fully offline against the published keys.

### Changed
- **Pricing** — self-serve tiers rebuilt as Starter $100 / Growth $300 /
  Grounded $600 (monthly, USD); prior prices archived. Existing subscribers
  are unaffected — they stay on the price they signed up under.
- **Reports** — the 12-month history window and CSV export now unlock on **any
  paid plan** (previously only the middle tier, which wrongly excluded the
  top tier), consistent with the evidence-bundle export beside them.
- **API reference** — the authentication example is now cross-platform
  (bash / zsh and Windows PowerShell). (#32)

### Fixed
- **Subscribe & billing buttons on production** — the live Stripe Customer
  Portal had no configuration, so every "Manage billing" click and every
  already-subscribed "Subscribe" redirect returned a 500. The portal is now
  configured and verified end-to-end. Billing handlers additionally log
  Stripe failures server-side instead of surfacing raw errors to users. (#33)
- **Evidence report step failing after a good drill** — drills that restored
  and passed every assertion were failing at the report step
  (`unwrap account key: message authentication failed`) for accounts whose
  encryption key was created before the persistent master key was set. The
  stale (unrecoverable) key rows were cleared; the new rotation tooling
  prevents recurrence.

### Security
- **License & copyright** — the repository is now explicitly proprietary /
  all-rights-reserved (© 2026 Ian Lee), with an **Apache-2.0 carve-out for
  `cmd/dokaz-verify`** so auditors retain an unambiguous right to build and
  run the independent evidence verifier. Per-file notices, `LICENSE`, and
  `NOTICE` added.
- **Team-scoped access control** — database, drill, and evidence access —
  including auditor share links and the evidence-bundle export — is now scoped
  to a member's teams and enforced in the data layer, not just the UI.
- **Evidence-key rotation** — closes the "lost or changed master key ⇒
  permanently unrecoverable evidence" risk by making key rotation a supported,
  no-data-loss operation.

## [2026-07-05]

### Added
- **Exec and Auditor roles** — an internal executive (read-only) role, and an
  external **Auditor** role: a real account for a compliance reviewer, scoped
  to drills, evidence, and targets, with no access to billing or the member
  roster.
- **Backup check-ins (heartbeats)** — a passive complement to drills. Register
  a monitor with an expected period, ping a URL from your backup job, and get
  alerted (webhook + audit event) when a check-in goes overdue.

### Changed
- **Pricing rebrand** — self-serve tiers renamed **Starter / Growth /
  Grounded**, daily drills on every tier, and the drills/day cap reframed as a
  workload budget.
- **Restore timeout** raised from 30 minutes to 6 hours to accommodate large
  dumps (surfaced by a new drill-stress harness).

### Fixed
- **Assertions on schema-qualified tables** — `row_count` / `no_nulls` now
  schema-qualify their target, and a "relation does not exist" failure is
  explained clearly instead of leaking a raw database error.

## [2026-05-25]

### Security
- **Hardening pass** — a security-review remediation across revenue- and
  data-safety-critical paths (idempotency, CSRF, and related fixes). See
  [`docs/security-audit-2026-05.md`](docs/security-audit-2026-05.md).

### Added
- **Full pg_dump format coverage** — the runner restores all four logical
  formats (plain, custom, tar, directory), detected from file content rather
  than the extension.

## [2026-05-22]

### Added
- **Public marketing surface** — landing page, a "how it works" explainer, and
  a public pricing page.
- **Subscriptions & billing** — Stripe subscription lifecycle (Checkout,
  Customer Portal, signature-verified webhooks), usage-based metering,
  staff-issued refunds, and per-tier plan limits.
- **14-day trial** (read-only until subscribed), scheduled recurring drills,
  and month-over-month drill reporting.
- **Per-drill Fly Machine sandbox** and an **S3 / R2 evidence store**
  (SigV4-signed, Object-Lock capable).
- **Staff admin panel** with SSO step-up and safe, reason-logged impersonation;
  **feature flags** (PostHog) and **Grafana dashboards as code**.

## [2026-05-21]

### Added
- **The 11-layer foundation** — the drill orchestrator and
  restore-and-assert pipeline; multi-tenant accounts with RBAC; perimeter
  security (CSRF, rate limiting, login throttle, signed webhooks); signed
  Ed25519 evidence with 7-year retention and GDPR data rights; observability
  (structured logs, metrics, tracing); transactional email + analytics; legal
  pages and a WCAG 2.2 AA pass; the staff support/admin panel; and the
  versioned `/v1` JSON API (API keys, idempotency, cursor pagination).
- **Identity & auth** — TOTP two-factor, passwordless magic-link login,
  Google + GitHub OAuth, and email verification.
- **Evidence integrity** — at-rest encryption with per-account crypto-shred,
  signing-key rotation via a verification-key set, and multiple assertion
  kinds per database.

## [2026-05-20]

- Project began as **Restore Drill**, the automated backup-verification product
  that became Dokaz (renamed Restore Drill → Soteria → Vesta → Dokaz). Earlier
  commits under a "Screen Dissector" prototype predate that pivot.
