# PostHog — product analytics + feature flags

## What it powers

- **Analytics** — funnel events (`user.signed_up`, `invitation.sent`,
  `drill.completed`, `drill.failed`) captured for growth dashboards.
- **Feature flags** — server-side flag evaluation (e.g. `self_serve_signup`),
  so a flag can be flipped from the PostHog UI without a deploy.

Both share one PostHog project. Without a key the app captures nothing and
flags fall back to env-driven defaults.

## Code status — complete

- `internal/analytics` — `PostHogAnalytics` posts events to `/capture` in a
  background goroutine (a failed send is dropped, never blocks a request).
- `internal/flags` — `PostHogFlags` evaluates flags via `POST /decide`,
  caches the result per distinct ID for 60s, and refreshes in the
  background. It **never blocks a request** on the network and falls back to
  the static env defaults (`FEATURE_*`) whenever PostHog has no answer.

## Setup

### 1. Create a PostHog project

1. Sign up at <https://posthog.com> (PostHog Cloud) — pick the **US** or
   **EU** region; that decides your host.
2. Create a project. From **Project settings**, copy the **Project API key**
   (starts `phc_...`).

### 2. Define feature flags

In **Feature flags**, create a flag whose key matches an app flag — currently
`self_serve_signup`. Set its rollout (e.g. 100%, or a condition). The app
reads it on the next evaluation (within ~60s, or immediately for a new
visitor).

Unknown flags, or any PostHog outage, fall through to the static default
(`self_serve_signup` defaults to **on**; override per-deploy with
`FEATURE_SELF_SERVE_SIGNUP=false`).

### 3. Environment variables

| Variable | Required | Value |
|---|---|---|
| `POSTHOG_API_KEY` | yes | Project API key (`phc_...`) |
| `POSTHOG_HOST` | if EU | `https://eu.posthog.com` (defaults to `https://app.posthog.com`) |

The same two variables drive both analytics and flags.

## Verify

1. Restart; the startup log should read `analytics enabled (posthog)`.
2. Sign up a test user — within a minute the `user.signed_up` event appears
   in PostHog's **Activity** / event explorer.
3. In PostHog, toggle the `self_serve_signup` flag off; within ~60s the
   `/signup` page should switch to the sales-led "request access" screen.
4. Toggle it back on and confirm signup reopens.
