# Go-live runbook

The ordered checklist to take Dokaz from "builds locally" to "a paying
customer can sign up." Per-integration detail lives in the sibling runbooks
(`stripe.md`, `postmark.md`, `s3.md`, `fly.md`, `signing-cert.md`); this is
the master sequence.

## 0. Accounts you need

- **Fly.io** — hosting (`flyctl` installed locally).
- **Neon** — managed Postgres (already provisioned).
- **Stripe** — billing.
- **Postmark** — transactional email.
- A **domain registrar** — for `dokaz.net` (or your chosen domain).

## 1. Generate the production secrets

Run locally; keep the output somewhere safe (a password manager).

```sh
openssl rand -base64 48                 # SESSION_KEY
openssl rand -base64 32                 # EVIDENCE_ENCRYPTION_KEY (32-byte, base64)
openssl genpkey -algorithm ed25519      # EVIDENCE_SIGNING_KEY (PKCS#8 PEM)
openssl rand -hex 16                    # METRICS_TOKEN (optional)
openssl rand -hex 16                    # POSTMARK_WEBHOOK_TOKEN
```

These never change once set — rotating them invalidates sessions / breaks
evidence verification. See `signing-cert.md` for the signing-key lifecycle.

## 2. Create the Fly app and the evidence volume

```sh
fly apps create dokaz
fly volumes create dokaz_evidence --region iad --size 1
```

The volume backs `EVIDENCE_DIR` (`/data/evidence`, set in `fly.toml`) so
signed PDFs survive deploys. At multi-machine scale, move to S3/R2 instead
(`EVIDENCE_S3_*` secrets — see `s3.md`).

## 3. Set the Fly secrets

```sh
fly secrets set \
  DATABASE_URL="postgresql://...neon.tech/neondb?sslmode=require" \
  SESSION_KEY="<step 1>" \
  EVIDENCE_ENCRYPTION_KEY="<step 1>" \
  METRICS_TOKEN="<step 1>" \
  POSTMARK_WEBHOOK_TOKEN="<step 1>" \
  EMAIL_FROM="notifications@dokaz.net" \
  STAFF_EMAILS="you@yourdomain.com"

# Multi-line PEM — set on its own:
fly secrets set EVIDENCE_SIGNING_KEY="$(openssl genpkey -algorithm ed25519)"
```

Stripe and Postmark secrets are added in the next two steps. Signup is open
by default (`self_serve_signup` defaults on) — no flag needed.

## 4. Stripe

Full detail in `stripe.md`. Summary:

1. Create two **Products** with recurring monthly **Prices** — Starter and Pro.
2. `fly secrets set STRIPE_SECRET_KEY=sk_test_... STRIPE_PRICE_STARTER=price_... STRIPE_PRICE_PRO=price_...`
   (use **test-mode** keys first).
3. Add a webhook endpoint → `https://<domain>/webhooks/stripe`, subscribe to
   `customer.subscription.*`, then `fly secrets set STRIPE_WEBHOOK_SECRET=whsec_...`.
4. Set `PRICE_STARTER_LABEL` / `PRICE_PRO_LABEL` to match the prices shown on
   the pricing page.

## 5. Postmark

Full detail in `postmark.md`. Summary:

1. Create a Postmark server; verify the **sending domain** (SPF + DKIM DNS
   records) so mail doesn't land in spam.
2. `fly secrets set POSTMARK_TOKEN=...`.
3. Point a Postmark bounce/complaint webhook at
   `https://<domain>/webhooks/postmark/<POSTMARK_WEBHOOK_TOKEN>`.

## 6. First deploy

```sh
fly deploy
```

`release_command` runs migrations first. Watch `fly logs`; `/readyz` must go
green. After this, pushes to `main` deploy automatically (the `deploy`
GitHub Action — set the `FLY_API_TOKEN` repo secret: `fly tokens create deploy`).

## 7. Domain

```sh
fly certs add dokaz.net
```

Add the DNS records Fly prints (A/AAAA + the ACME CNAME). One domain serves
everything — marketing pages at `/`, the app behind login.

## 8. Smoke test (still in Stripe test mode)

1. Sign up; confirm the verification email arrives (Postmark).
2. Connect a database (a small `pg_dump -Fc` file), add an assertion.
3. Run a drill; watch it pass; download the signed PDF.
4. Set a daily schedule; confirm `/reports` shows the drill.
5. Subscribe with Stripe test card `4242 4242 4242 4242`; confirm the plan
   flips and the Customer Portal opens.
6. Restart the app (`fly apps restart dokaz`); re-download the PDF — it
   must still verify (proves the persistent evidence key + volume work).

## 9. Go live

Swap Stripe to **live** keys (`STRIPE_SECRET_KEY=sk_live_...`, live price IDs,
live `STRIPE_WEBHOOK_SECRET`) and redeploy.

## Still open (not blockers, but do them soon)

- **Legal** — have counsel review the Terms/Privacy/DPA and fill in the
  operating entity + governing-law jurisdiction.
- **Drill isolation** — drills currently restore into the app's Postgres.
  Fine for early/small design partners; configure the Fly Machines runner
  (`FLY_API_TOKEN`, `FLY_APP_NAME` — see `fly.md`) before larger databases.
- **Observability** — set `SENTRY_DSN` and `POSTHOG_API_KEY` (see
  `sentry.md`, `posthog.md`).
- **Evidence at scale** — move from the Fly volume to S3/R2 with Object Lock
  (`s3.md`) when you run more than one machine.
