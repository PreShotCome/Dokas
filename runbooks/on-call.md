# Runbook: On-Call

How on-call works for Soteria. Pairs with
`runbooks/incident-response.md` (what to do during an incident) and
`runbooks/secret-rotation.md`.

## Who is on call

Solo phase: the founder. As the team grows, a weekly rotation; hand-off
every Monday with a short written sync of anything still smouldering.

## What pages you

Auto-paging is deliberately narrow — most problems can wait for business
hours. A page fires for:

- `AppDown` — `/healthz` failing for >1m (SEV-1).
- `DrillFailureRateHigh` — >25% of drills failing over 15m (SEV-2).
- `DrillQueueBacklog` — River queue depth >50 for 10m (SEV-2).

Everything else (latency, webhook failures, 5xx rate) raises a non-paging
alert that waits for business hours. Alert definitions live in
`dashboards/alerts.yml`.

## Quiet hours

Drills are scheduled in each customer's business hours by default, so the
drill pipeline rarely misbehaves overnight. Outside our own business hours
only SEV-1 signals page; SEV-2/3 are deferred to the next morning. A
customer on a future 24/7 support tier would change this — gate that on a
plan flag when it exists.

## Acknowledging a page

1. Acknowledge within 5 minutes so it doesn't escalate.
2. Open `runbooks/incident-response.md` and follow the first-five-minutes
   checklist.
3. If it's a false alarm, still note it — repeated false alarms mean an
   alert threshold needs tuning (`dashboards/alerts.yml`).

## Tools

- **Admin panel** (`/admin`, staff only) — user lookup, safe impersonation
  (always with a reason), drill replay, evidence regeneration.
- **Metrics** — `/metrics` (Prometheus), the Grafana dashboard
  (`dashboards/soteria.json`).
- **Logs** — structured JSON; filter by `level`, `trace_id`, `account_id`.
- **Traces / errors** — the OTLP collector and Sentry, when configured.

## Hand-off checklist

- Any open incidents or degraded components?
- Any alerts that fired and were silenced rather than fixed?
- Any customer follow-ups promised?
- Anything deferred in `docs/backlog.md` that became urgent?

## Escalation

Solo phase has no secondary. If you are unreachable, the documented
fallback is the status page: mark the affected component and post the
"investigating" template so customers aren't in the dark. Fix on return.
