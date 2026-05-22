# Stripe — subscription billing

## What it powers

The subscription lifecycle: an account owner subscribes to a plan via Stripe
Checkout, manages or cancels it in the Stripe Customer Portal, and a
signature-verified webhook keeps the account's `plan` and
`subscription_status` in sync with Stripe. No card data touches our servers.

## Code status — complete (lifecycle)

- `internal/billing` — `Service` talks to Stripe's REST API directly (no
  SDK): `Create` (customer), `Checkout`, `Portal`, `ListCharges`/`Refund`,
  `ReportUsage`, plus webhook signature verification and event parsing.
- `POST /account/billing/checkout` / `POST /account/billing/portal` — billing
  actions, gated on the `billing.write` RBAC action.
- `POST /webhooks/stripe` — verifies the `Stripe-Signature` header, then
  syncs `customer.subscription.created/updated/deleted` to the account.
- Without `STRIPE_SECRET_KEY` the package is a no-op; the account page shows
  "Billing is not configured".

Plan enforcement (per-tier resource caps) and usage-based billing are both
wired — see the plan-enforcement entry in `docs/backlog.md` and the
"Usage-based billing" section below.

## Setup

### 1. Stripe account, products, and prices

1. Create a Stripe account; do the steps below in **Test mode** first.
2. **Products** → create a product per tier (e.g. `Starter`, `Pro`), each
   with a **recurring price**. Copy each **Price ID** (`price_...`).
3. **Developers → API keys** → copy the **Secret key** (`sk_test_...`, later
   `sk_live_...`).

### 2. Customer Portal

**Settings → Billing → Customer portal** → activate it and choose what
customers may do (update payment method, cancel, switch plan, see invoices).
The portal is required for the "Manage billing" button.

### 3. Webhook

1. **Developers → Webhooks → Add endpoint**.
2. Endpoint URL: `https://YOUR_APP_HOST/webhooks/stripe`.
3. Select events: `customer.subscription.created`,
   `customer.subscription.updated`, `customer.subscription.deleted`.
4. Copy the endpoint's **Signing secret** (`whsec_...`).

### 4. (Optional) Stripe Tax

Enable **Settings → Tax** if you need automatic sales-tax/VAT. This is
dashboard-side configuration; no app change is needed.

### 5. (Optional) Usage-based billing

The app reports one **meter event** per drill run, so a plan can charge for
drill volume on top of (or instead of) the flat tier price.

1. **Billing → Meters → Create meter**. Set the **event name** (e.g.
   `drill_run`) and aggregation **sum** over `payload[value]`. The app sends
   `payload[stripe_customer_id]` and `payload[value]=1` per drill.
2. Add a **metered price** to a product, linked to that meter. Stripe's
   price config — a free tier, then a per-unit or graduated rate — decides
   the *included allowance*; the app only reports raw usage.
3. Set `STRIPE_METER_EVENT` to the meter's event name. Unset, usage
   reporting is off and only flat subscription billing applies.

Each meter event carries the drill ID as its `identifier`, so a retried
teardown counts the drill once.

### 6. Environment variables

| Variable | Required | Value |
|---|---|---|
| `STRIPE_SECRET_KEY` | yes | Secret key from step 1 |
| `STRIPE_WEBHOOK_SECRET` | yes | Webhook signing secret from step 3 |
| `STRIPE_PRICE_STARTER` | yes | Price ID for the Starter plan |
| `STRIPE_PRICE_PRO` | yes | Price ID for the Pro plan |
| `STRIPE_METER_EVENT` | no | Meter event name for usage billing (e.g. `drill_run`) |

## Verify

1. Restart; the startup log should read `billing enabled (stripe)`.
2. On the Account page, **Billing** shows "Subscribe — Starter / Pro".
   Subscribe; on Stripe's page pay with test card `4242 4242 4242 4242`.
3. Confirm the webhook delivered (Stripe Dashboard → Webhooks → the endpoint
   shows a 200) and the account's plan badge updated.
4. Click **Manage billing** → the Customer Portal opens. Cancel the
   subscription and confirm the plan drops back to `trial`.
5. Go live: swap the test keys/prices/webhook for live-mode values.
