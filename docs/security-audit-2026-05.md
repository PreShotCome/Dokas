# Security & Correctness Audit — May 2026

Tracking the 40 findings from the deep-scan audit run on 2026-05-23, plus
2 carryover items from the prior scan. Source commit: `619bbd2`.

## Legend

- ✅ **FIXED** — code change shipped, build/tests green
- 🟡 **ACCEPTED** — known issue, not addressed this cycle (with reason)
- ⏳ **PLANNED** — queued (used during work, removed at completion)

## Critical — data, billing, security

| ID  | Title | File | Status | Commit |
|-----|-------|------|--------|--------|
| N1  | Trial gating bypass via `/v1/*` API keys | `internal/web/handlers/v1.go` | ✅ | (Phase 1) |
| N2  | Stripe webhook has no event-id dedup or ordering guard | `internal/billing/webhook.go` | ✅ | (Phase 1) |
| N3  | `subscription.deleted` never restores `trial_ends_at` → cancelled = locked out | `internal/web/handlers/billing.go` | ✅ | (Phase 1) |
| N4  | Past-due / unpaid subs stay on PlanPro | `internal/billing/webhook.go` | ✅ | (Phase 1) |
| N5  | Subscription price read from line item `[0]` — multi-item subs demote | `internal/billing/webhook.go` | ✅ | (Phase 1) |
| N6  | AES-GCM wrapped DEKs not bound to account_id — cross-account evidence swap | `internal/evidence/cipher.go` | ✅ | (Phase 1) |
| N7  | Magic-link / OAuth URLs built from attacker-controlled Host header | `internal/web/handlers/account.go` | ✅ | (Phase 2) |

## High — auth / takeover surface, runner correctness

| ID   | Title | File | Status | Commit |
|------|-------|------|--------|--------|
| N8   | OAuth callback swaps an authenticated user's identity | `internal/web/handlers/oauth.go` | ✅ | (Phase 2) |
| N9   | Magic-link consume hijacks an already-authenticated session | `internal/web/handlers/magiclink.go` | ✅ | (Phase 2) |
| N10  | Self-serve-signup flag bypassed via OAuth | `internal/oauth/oauth.go` | ✅ | (Phase 2) |
| N11  | TOTP enrollment code replayable as first MFA login | `internal/web/handlers/mfa.go` | ✅ | (Phase 2) |
| N12  | Fly machine leaked on `SetSandboxDB` failure | `internal/drill/steps/steps.go` | ✅ | (Phase 3) |
| N13  | Provision cleanup uses cancelled context → orphans machine | `internal/drill/steps/steps.go` | ✅ | (Phase 3) |
| N14  | Scheduled drills drift forward every tick | `internal/drill/scheduler.go` | ✅ | (Phase 3) |
| N15  | No `FOR UPDATE SKIP LOCKED` on `DueTargets` | `internal/drill/store.go` | ✅ | (Phase 3) |
| N16  | Persistent evidence volume + autoscaling = lost evidence | `fly.toml` | ✅ | (Phase 1) |
| N17  | `billingCheckout` always creates a new sub — double-charge risk | `internal/web/handlers/billing.go` | ✅ | (Phase 1) |
| N18  | `MonthlyStats` month-boundary off-by-one in partial-month buckets | `internal/drill/store.go` | ✅ | (Phase 3) |
| N19  | Swallowed `RecordAttempt` errors (carryover) | `internal/webhooks/worker.go` | ✅ | (Phase 1) |
| N20  | Magic-link is per-IP throttled, not per-target email | `internal/web/handlers/magiclink.go` | ✅ | (Phase 2) |

## Medium — correctness, polish, enumeration

| ID   | Title | File | Status | Commit |
|------|-------|------|--------|--------|
| N21  | Webhook `event_key` collision + River uniqueness window | `internal/webhooks/dispatch.go` | ✅ | (Phase 4) |
| N22  | `event_key` reads `data["drill_id"]` via fragile string assert | `internal/webhooks/dispatch.go` | ✅ | (Phase 4) |
| N23  | OAuth has no PKCE, no nonce, identity from `/userinfo` | `internal/oauth/oauth.go` | ✅ | (Phase 4) |
| N24  | `completeStaffSSO` trusts session-cached `is_staff` | `internal/web/handlers/oauth.go` | ✅ | (Phase 4) |
| N25  | Trial migration retroactively lapses long-existing dev accounts | `migrations/20260521000019_account_trials.sql` | ✅ | (Phase 4) |
| N26  | `robots.txt` blocks marketing pages | `internal/web/handlers/growth.go` | ✅ | (Phase 4) |
| N27  | Reports handler swallows DB errors — blank report on outage | `internal/web/handlers/reports.go` | ✅ | (Phase 4) |
| N28  | Magic-link request timing leaks user enumeration | `internal/web/handlers/magiclink.go` | ✅ | (Phase 4) |
| N29  | PDF "Verdict: FAILED" for skipped drills | `internal/report/pdf.go` | ✅ | (Phase 4) |
| N30  | `oauthStateCookie` shared between social & staff SSO | `internal/web/handlers/oauth.go` | ✅ | (Phase 4) |

## Low — nits and cosmetic

| ID   | Title | File | Status | Commit |
|------|-------|------|--------|--------|
| N31  | MagicLink templates pass `user=nil` to layout | `internal/web/templates/magiclink.templ` | ✅ (covered by N9) | (Phase 2) |
| N32  | reports.templ "Databases" tile counts all targets | `internal/web/templates/reports.templ` | ✅ | (Phase 5) |
| N33  | `mfaChallengeSubmit` doesn't clean prior `mfa_pending` rows | `internal/web/handlers/auth.go` | ✅ | (Phase 5) |
| N34  | `TrialActive` fail-open when `trial_ends_at IS NULL` | `internal/account/trial.go` | ✅ | (Phase 5) |
| N35  | No retries on Fly API calls | `internal/fly/fly.go` | ✅ | (Phase 5) |
| N36  | Postgres image not pinned by digest | `internal/runner/fly_machine.go` | 🟡 ACCEPTED — operational | docs only |
| N37  | `WaitStarted` server timeout 60s vs HTTP client 30s | `internal/fly/fly.go` | ✅ | (Phase 5) |
| N38  | `reportsExport` ignores csv Writer errors | `internal/web/handlers/reports.go` | ✅ | (Phase 5) |
| N39  | No stripe_events poison-pill handling | `internal/web/handlers/billing.go` | ✅ | (Phase 5) (covered by N2) |
| N40  | Layout trialBannerText "0 days left" cosmetic | `internal/web/templates/layout.templ` | ✅ | (Phase 5) |

## Carryover from prior scan

| #   | Title | File | Status | Commit |
|-----|-------|------|--------|--------|
| #11 | CSRF first-POST after cookie loss always 403s | `internal/web/csrf.go` | ✅ | (Phase 1) |
| #19 | Swallowed `RecordAttempt` errors (= N19) | `internal/webhooks/worker.go` | ✅ | (Phase 1) |

## Summary

- **Total findings:** 42 (40 new + 2 carryover)
- **Critical fixed:** 7 / 7
- **High fixed:** 13 / 13
- **Medium fixed:** 10 / 10
- **Low fixed:** 9 / 10
- **Accepted (operational, not code):** 1 — N36 (digest-pin the sandbox
  Postgres image via `FLY_POSTGRES_IMAGE=postgres:16-alpine@sha256:...`
  in production; doc-only change, no code fix.)

Work delivered in 5 phases:

1. **Phase 1 — Revenue & data safety** (N1–N6, N16, N17, N19, CSRF #11) — 10 fixes
2. **Phase 2 — Auth & takeover surface** (N7, N8, N9, N10, N11, N20) — 6 fixes
3. **Phase 3 — Runner & scheduler correctness** (N12, N13, N14, N15, N18) — 5 fixes
4. **Phase 4 — Mediums** (N21–N30) — 10 fixes
5. **Phase 5 — Lows** (N31–N40) — 10 fixes
