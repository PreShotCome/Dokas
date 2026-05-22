# Sentry — error tracking

## What it powers

Off-box error tracking. The panic-recovery middleware reports any handler
panic to Sentry (with the request path and `trace_id`), and unexpected
errors elsewhere can be sent via the error reporter. Without a DSN the app
uses a no-op reporter that only logs — fully functional, just not reported.

## Code status — complete

- `internal/obs` — `ErrorReporter` is implemented by `SentryReporter`
  (real `sentry-go` SDK) and `NoopReporter`. `NewErrorReporter` returns the
  Sentry one when `SENTRY_DSN` is set, the no-op otherwise.
- `Provider.Recoverer` is the router's panic middleware — it captures the
  panic to Sentry, then returns 500.

## Setup

1. Create a Sentry account and a **project** — platform **Go**.
2. From **Project Settings → Client Keys (DSN)**, copy the **DSN**.

### Environment variables

| Variable | Value |
|---|---|
| `SENTRY_DSN` | The project DSN |
| `ENV` | Deployment environment (`dev`/`staging`/`prod`) — tags every event |

`ENV` already drives other behaviour (e.g. secure cookies); Sentry just
tags events with it so environments are filterable in the Sentry UI.

## Verify

1. Restart; with `SENTRY_DSN` set, error reporting is live (the no-op
   reporter is only used when the DSN is empty).
2. Trigger an error — e.g. hit an endpoint that panics in a test build, or
   temporarily add a panic — and confirm the event appears in Sentry,
   tagged with the environment, the request path, and the `trace_id`.
3. Remove any test panic afterwards.
