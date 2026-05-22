# Integration runbooks

Restore Drill degrades gracefully without third-party services — every
external integration has a no-op / local fallback so dev and CI run with no
accounts. These runbooks cover activating the real service in production:
the account to create, the dashboard/DNS configuration, the environment
variables to set, and how to verify.

Each service is independent — activate them in any order.

| Service | Purpose | Code status | Runbook |
|---|---|---|---|
| Postmark | Transactional email | ✅ complete | [postmark.md](postmark.md) |
| Stripe | Billing / subscriptions | ✅ complete | [stripe.md](stripe.md) |
| Fly Machines | Per-drill cloud sandbox | ⬜ pending | — |
| S3 / R2 Object Lock | Evidence storage | ⬜ pending | — |
| Google + GitHub OAuth | Social login | ✅ complete | [oauth.md](oauth.md) |
| PostHog | Analytics + feature flags | ✅ complete | [posthog.md](posthog.md) |
| OpenTelemetry / Grafana | Traces, metrics, logs | ✅ complete | [observability.md](observability.md) |
| Sentry | Error tracking | ✅ complete | [sentry.md](sentry.md) |
| Document-signing certificate | Evidence trust chain | ⬜ pending | — |
| RFC 3161 timestamp authority | Evidence timestamping | ⬜ pending | — |

"Code status" is whether the in-repo integration is implemented. A ✅ service
works the moment its environment variables are set; nothing else to build.
