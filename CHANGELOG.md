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
