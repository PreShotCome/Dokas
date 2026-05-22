# Runbook: Secret Rotation

Scope: how to rotate every secret Soteria depends on. Rotate on a
quarterly cadence and immediately on any suspected compromise or staff
offboarding.

All secrets are injected as environment variables (Fly secrets in
staging/prod, `.env`/shell locally). The app never reads secrets from disk
or the database except the per-endpoint webhook secrets, which are covered
below.

## Inventory

| Secret | Env var | Blast radius if leaked |
|---|---|---|
| Session signing/marker key | `SESSION_KEY` | Session forgery |
| Database credentials | `DATABASE_URL` | Full data access |
| Stripe secret key | `STRIPE_SECRET_KEY` | Billing API access |
| Webhook endpoint secrets | per-row in `webhook_endpoints.secret` | Customer can be sent forged events |

## General procedure

1. Generate the new secret value.
2. Set it alongside the old one if the component supports dual-read; or set
   it and accept a brief restart.
3. Deploy / restart so the new value is live.
4. Revoke the old value at the provider.
5. Record the rotation date in the team log.

## SESSION_KEY

1. Generate: `openssl rand -base64 48`.
2. `fly secrets set SESSION_KEY=<new>` — this triggers a rolling restart.
3. Existing sessions: tokens are random and stored hashed in `sessions`, so
   they survive a `SESSION_KEY` change (the key is not the session HMAC in
   the current design). No forced logout needed.
4. If the old key is believed compromised, additionally truncate active
   sessions: `DELETE FROM sessions;` — this logs everyone out.

## DATABASE_URL

1. Provision a new database role / rotate the password with the DB provider
   (Neon: create a new role or reset the password).
2. `fly secrets set DATABASE_URL=<new>`.
3. Confirm the app reconnects (watch `/healthz` and logs).
4. Drop or disable the old role.

## STRIPE_SECRET_KEY

1. In the Stripe dashboard, roll the API key (Developers → API keys).
2. `fly secrets set STRIPE_SECRET_KEY=<new>`.
3. Stripe keeps the old key valid for a short overlap window — verify a
   customer-create works on the new key, then revoke the old one.
4. If `STRIPE_SECRET_KEY` is unset the billing layer degrades to a no-op;
   that is safe but disables customer creation.

## Webhook endpoint secrets

Each `webhook_endpoints` row has its own `secret` used to HMAC-sign
deliveries. There is no in-app rotation UI yet (planned). To rotate manually:

1. The customer creates a new endpoint (gets a fresh secret), updates their
   receiver to accept the new secret, then deletes the old endpoint.
2. Operator-side emergency rotation: `UPDATE webhook_endpoints SET secret =
   'whsec_' || encode(gen_random_bytes(24), 'base64') WHERE id = '<id>';`
   then notify the customer out-of-band — in-flight deliveries already
   enqueued will sign with whatever secret is current at delivery time.

## Notes / known gaps

- The webhook delivery worker does **not** yet block private/loopback
  target IPs (SSRF). Until that lands, treat customer-supplied webhook URLs
  as semi-trusted and keep the worker's egress network policy tight.
- There is no automated expiry alerting yet; the quarterly calendar
  reminder is the control.
