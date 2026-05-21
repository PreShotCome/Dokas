# Runbook: Incident Response

How Restore Drill handles a production incident. Keep this short enough to
follow at 3am.

## Severity levels

| SEV | Definition | Examples | Response |
|---|---|---|---|
| SEV-1 | Customer-facing outage or data-integrity risk | App down, drills silently failing, evidence unreadable | Drop everything, page, status page red |
| SEV-2 | Major degradation, no data risk | Drill queue badly backed up, webhooks not delivering, p95 latency way over SLO | Same-day, status page degraded |
| SEV-3 | Minor / contained | One customer's drill stuck, a non-critical metric missing | Next business day |

## First five minutes

1. **Acknowledge.** Post in the incident channel: "investigating <symptom>".
2. **Classify.** Pick a SEV from the table. When unsure, round up.
3. **Check the obvious signals:**
   - `GET /healthz` — process up?
   - `GET /readyz` — dependencies (DB) reachable?
   - `GET /metrics` — `river_jobs_available` (queue depth), `drills_total`,
     `http_requests_total` by status.
   - Structured logs — filter by `level=ERROR`; each line carries
     `trace_id`, `request_id`, `account_id`.
   - Traces — follow the `trace_id` from a failing request through the
     drill span tree.
   - Sentry — recent unhandled errors / panics.
4. **Communicate.** SEV-1/2: update the status page.

## Common incidents

### App is down (`/healthz` fails)
- Check the platform (Fly) dashboard for crashed/OOM machines.
- Check recent deploys — roll back per `runbooks/secret-rotation.md`'s
  rollback note (`fly deploy --image <prev>`).

### Not ready (`/healthz` ok, `/readyz` 503)
- The body names the failing check (currently `database`).
- Check the Postgres provider (Neon) status and connection limits.

### Drill queue backed up (`river_jobs_available` climbing)
- A stuck or slow step worker, or Postgres pressure.
- Inspect `river_job` for jobs in `retryable`/`running` with high attempts.
- A poison job: cancel it via River, open a SEV-3 to fix the root cause.

### Webhooks not delivering
- `webhook_deliveries_total{outcome="failed"}` rising.
- Usually a customer endpoint down — not our incident. Confirm by checking
  delivery rows' `last_error`/`last_status_code`.

### Evidence signature shows "invalid"
- SEV-1 — data integrity. Either the stored PDF was altered or the signing
  key changed. Check whether `EVIDENCE_SIGNING_KEY` was rotated without
  re-signing existing evidence; check object-store integrity.

## Comms templates

**Status page (SEV-1):**
> We are investigating an issue affecting <area>. Drills may be delayed.
> Next update in 30 minutes.

**Resolution:**
> Resolved at <time> UTC. Root cause: <one line>. A postmortem will follow.

## Postmortem template

Open within 48h of any SEV-1/2. Blameless.

```
# Postmortem: <short title> — <date>
## Summary
## Impact            (who, how long, how many drills/customers)
## Timeline          (UTC, detection → mitigation → resolution)
## Root cause
## What went well
## What went wrong
## Action items      (owner, due date, tracking link)
```

## On-call notes (solo phase)

- Drills are scheduled in customer business hours by default, so the
  drill pipeline rarely pages overnight.
- Auto-paging is limited to SEV-1 signals (`/healthz` failing, drill
  failure-rate spike). Everything else waits for business hours.
