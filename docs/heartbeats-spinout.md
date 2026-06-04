# Spin-out blueprint: heartbeats as a standalone product

Status: **planning only.** Heartbeats currently ship as a feature *inside*
Soteria. This doc is the shelf-ready plan for lifting them into their own SaaS
*if* the in-Soteria upsell shows demand. Don't execute it before a paying
customer asks for standalone check-ins — first dollar beats a second codebase.

## Why it could stand alone

- **Bigger market.** Soteria sells to teams needing backup-audit evidence
  (niche). A cron/heartbeat monitor sells to *anyone with a scheduled job* —
  cron, CI, ETL, queue workers, IoT.
- **Dead-simple value prop.** "Tell me when my cron dies." No restore, no
  evidence chain, no Postgres knowledge.
- **Proven category.** Healthchecks.io is a well-known profitable solo-dev
  business (open source, even) — direct evidence the shape works alone.

## Suggested name

Keep the Greek convention. Candidates: **Argus** (the all-seeing
hundred-eyed watchman — on-brand for "always watching your jobs") or
**Heimdall**-equivalent. Recommended: **Argus** (`preshotcome/argus`).

## What ports cleanly (the engine)

`internal/heartbeat/` is already free of Soteria-specific assumptions beyond an
`account_id` FK and the `accounts` lapsed-trial check in the sweep query:

- `store.go` — monitors, `RecordPing`, atomic `MarkOverdueDown`, ping log,
  pause/resume/delete, prune. **Moves as-is** (drop the `accounts` join in
  `MarkOverdueDown`, or keep it against the new product's own accounts table).
- `sweeper.go` — `SweeperWorker` + `SweeperPeriodicJob` + the `Dispatcher` /
  `Auditor` / `Notifier` interfaces. **Moves as-is.**
- `notify/notify.go` — interface-based; **moves as-is** once the new shell
  provides a `Sender` + `Accounts`.
- The migration `…023_heartbeats.sql`. **Moves as-is.**

## What must be rebuilt (the shell)

Everything around the engine is Soteria's. Two routes:

**A. Greenfield (cleaner, more work).** New repo, copy the proven Soteria
patterns (they're deliberately boring and portable):

- Auth: hand-rolled Postgres sessions (`internal/auth`) — copy wholesale.
- Billing: `internal/billing` (Stripe + Noop) — copy; new price IDs.
- Accounts/RBAC: copy `internal/account` + `internal/auth/rbac.go`.
- Web shell: Chi router, `csrf`, `ratelimit`, templ `layout.templ`, `render`.
- The public ping endpoint + dashboard handlers/templates from
  `web/handlers/heartbeats.go` + `templates/heartbeats.templ`.
- Marketing: a small Astro site (mirror the planned Soteria marketing repo).

**B. Extract a shared base module (less duplication, more coupling).** Pull
`auth`, `account`, `billing`, `email`, `ratelimit`, `csrf`, `obs` into a
`preshotcome/saas-core` module both Soteria and Argus import. Higher upfront
cost; pays off only if you run ≥2 products on it. **Defer to route A first.**

## Product differences to design in

- **Pricing by monitor count + check-in frequency**, not drill cadence. Free
  tier (e.g. 3 monitors, hourly resolution) is the acquisition engine — a real
  free tier matters more here than in Soteria (sales-led).
- **More alert channels.** Email (done), then Slack/Discord/PagerDuty webhooks,
  SMS (Twilio) as a paid axis.
- **Self-serve signup on by default** (Soteria gates it behind a flag).
- **Cron-expression schedules**, not just period+grace (backlog item already).
- **Public status badges** ("last check-in" SVG) — a built-in referral loop,
  same trick as Soteria's "Verified by" PDF footer.

## Rough sequence (when triggered)

1. `preshotcome/argus` repo; copy the Soteria shell (route A), strip drills /
   evidence / assertions.
2. Drop in `internal/heartbeat` + `notify` + the migration.
3. New Stripe products; free tier + 1–2 paid tiers; gate monitor count +
   channels via the `Limits` pattern.
4. Astro landing page; the `curl` one-liner front and centre.
5. Slack/Discord alert channels (highest-demand after email).
6. Public status badges (the growth loop).

## Backlog carried over

Per-plan monitor limits (done in Soteria), cron-expression schedules,
SSRF-hardened webhook targets, alert channels beyond email/webhook.
