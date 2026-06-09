# Selket — Launch Readiness Log

Selket exists to produce evidence. This document is Selket's own evidence
that it was, in fact, made ready for launch — entry by entry, with
commit hashes and verifiable claims.

Each section below tracks one item from the launch audit. An item is
not "done" until there's a row in its log table linking the work to a
commit or to an external attestation (DNS record, third-party
confirmation email, signed receipt).

**Status legend**

| Status | Meaning |
|---|---|
| ⬜ TODO | Identified, not started |
| 🟡 IN PROGRESS | Work begun, not finished |
| ✅ DONE | Completed with evidence cited below |
| 🟦 EXTERNAL | Blocked on a third party (Stripe, IRS, Postmark, registrar); evidence is an attestation, not a commit |
| ❌ N/A | Determined unnecessary — explained below |

The bar for ✅: *an auditor looking at this row could verify the claim
without taking anyone's word for it.*

---

## Summary

| # | Item | Status | Last update |
|---|---|---|---|
| 1 | Merge `claude/compassionate-gauss-Awq2c` → `main` | 🟡 IN PROGRESS | this commit |
| 2 | LLC EIN + business bank + Stripe activation | ⬜ TODO | — |
| 3 | Stripe products + webhook + price IDs as Fly secrets | ⬜ TODO | — |
| 4 | Postmark account + selket.io sender domain (DKIM/SPF/DMARC) | ⬜ TODO | — |
| 5 | DNS for selket.io | ⬜ TODO | — |
| 6 | First Fly deploy with production secrets | ⬜ TODO | — |
| 7 | Production Postgres + automated backups | ⬜ TODO | — |
| 8 | Public signing-key endpoint at `.well-known/evidence-signing-keys.pem` | ✅ DONE | 2026-06-09 |
| 9 | `selket-verify` CLI binaries on GitHub Releases | ⬜ TODO | — |
| 10 | Terms / Privacy / DPA — rebranded + sub-processor list | ⬜ TODO | — |
| 11 | Evidence-key backup procedure (signing + encryption) | ⬜ TODO | — |
| 12 | Status page at `status.selket.io` | ⬜ TODO | — |
| 13 | `security@selket.io` + `SECURITY.md` vulnerability disclosure | ⬜ TODO | — |
| 14 | `support@selket.io` forward | ⬜ TODO | — |
| 15 | Sentry wired to a real DSN | ⬜ TODO | — |
| 16 | Fly volume snapshots or S3/R2 evidence storage | ⬜ TODO | — |
| 17 | Customer-facing data-loss response runbook | ⬜ TODO | — |
| 18 | Selket-specific logo / favicon (vs. inherited phoenix) | ⬜ TODO | — |
| 19 | GDPR data-deletion endpoint | ⬜ TODO | — |
| 20 | Onboarding fixtures + walkthrough | ⬜ TODO | — |
| 21 | Stripe purchase flow verified end-to-end against real keys | ⬜ TODO | — |
| 22 | OAuth providers (Google, GitHub) | ⬜ TODO | — |
| 23 | Customer-facing audit-trail viewer | ⬜ TODO | — |

---

## 1. Merge dev branch to `main`

The six end-to-end bug fixes uncovered during the onboarding walkthrough
(see commits 3089ce9, 4a185f8, 4e79947, 3c23c48, bc319d1, 359ddfb) plus
the e2e-smoke harness (08868d8) live on `claude/compassionate-gauss-Awq2c`.
Until merged into `main`, neither the production deploy workflow nor any
future contributor sees the fixes.

| When | What | Evidence |
|---|---|---|
| 2026-06-09 | Full local CI gauntlet passes pre-merge: `templ generate`, `go mod tidy` (no diff), `go vet`, `govulncheck` (0 in code path), `tailwindcss --minify`, `go test ./...` (all packages green incl. `TestV1DatabasePlanLimit` after a trial-end-date fix), `go build` of all five binaries | This commit |
| 2026-06-09 | `TestV1DatabasePlanLimit` updated to seed `trial_ends_at` when flipping a test account to `trial` — `TrialLapsed` correctly treats a null end date as lapsed, so the test as written would 402 before reaching the cap | This commit |

---

## 8. Public signing-key endpoint

Selket's entire value proposition is that its evidence verifies
independently of Selket. That promise is empty if the verifying public
key is only available from Selket's own tooling. The keys must be
published at a stable, well-known URL so a customer — or their auditor,
or a court — can fetch them and verify a detached signature with stock
tooling years after the fact, even if Selket is gone.

`GET /.well-known/evidence-signing-keys.pem` now serves the active
signing key plus every retired verification key as a single PEM
document. Each block is preceded by `# PublicKeyID:` and `# Status:`
(active/retired) comment lines placed *outside* the PEM block, because
header lines inside a block break OpenSSL and Go's `pem.Decode`, while
text between blocks is ignored as preamble. The endpoint is public,
CSRF-exempt (top-level, outside the session group), and sent with a
one-hour cache so a rotation propagates within the day.

| When | What | Evidence |
|---|---|---|
| 2026-06-09 | `(*Signer).AllPublicKeysPEM()` added in `internal/evidence/sign.go`; `*evidence.Signer` wired through `handlers.Deps`/`Handlers` and `cmd/server/main.go`; route mounted next to `robots.txt` in `handlers.go` | This commit |
| 2026-06-09 | Round-trip verified: the served PEM piped through `openssl pkey -pubin -text -noout` recovers a 32-byte `ED25519 Public-Key`, and the `# PublicKeyID` comment equals `Signer.PublicKeyID()` (the fingerprint a PDF signature carries) | Local test run |

---

## 11. Evidence-key backup procedure

When Selket's `EVIDENCE_SIGNING_KEY` is lost, every PDF the product has
ever issued becomes unverifiable forever. When `EVIDENCE_ENCRYPTION_KEY`
is lost, every PDF becomes unreadable forever. These are the two
existential single points of failure of the entire product. The backup
procedure for them must exist and be tested before launch.

Procedure (followed verbatim before the first production deploy):

1. Generate the keys exactly once via `go run ./cmd/devkeys` in a
   trusted environment.
2. Set them as Fly secrets so the running app can read them.
3. Print the two values, sealed, to paper — in two different physical
   locations.
4. Verify the printed copies by re-typing one of them back into a
   throwaway server and confirming a previously-issued PDF still
   verifies.

This row stays open until evidence is filed: a dated note signed by the
person who performed the procedure, with the two storage locations
named and the verification result. The keys themselves never appear in
this log; only the attestation does.

| When | What | Evidence |
|---|---|---|
| — | — | (pending first production key generation) |
