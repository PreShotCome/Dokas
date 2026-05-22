# Postmark — transactional email

## What it powers

Every transactional email Soteria sends: team invitations, email
verification, passwordless magic-link sign-in, and the welcome message.
It also receives Postmark's bounce / spam-complaint webhooks and adds those
addresses to a suppression list so a bounced or complaining recipient is
never emailed again.

## Code status — complete

The integration is fully implemented; activation is configuration only.

- `internal/email` — `PostmarkMailer` sends via Postmark's REST API;
  `Mailer.Send` checks the `email_suppressions` table first.
- `POST /webhooks/postmark/{token}` — `postmarkBounce` records bounced and
  complained addresses (constant-time token check on the path segment).
- Without `POSTMARK_TOKEN` the app uses `LogMailer`, which writes the
  rendered message — including any link — to the server log. That is how
  local dev "receives" invitation and magic-link emails.

## Setup

### 1. Create a Postmark account and Server

1. Sign up at <https://postmarkapp.com>.
2. Create a **Server** (Postmark's term for a sending environment) — e.g.
   `Soteria — Production`.
3. Open the server → **API Tokens** → copy the **Server API Token**. This is
   `POSTMARK_TOKEN`.

### 2. Verify your sending domain

The `From` address must be a confirmed sender, or Postmark rejects the send.

1. **Sender Signatures** → add your domain (e.g. `soteria.io`).
2. Add the **DKIM** and **Return-Path** DNS records Postmark shows you. The
   Return-Path record is what gives SPF alignment — Postmark walks you
   through it.
3. Add a **DMARC** record for the domain (`p=quarantine` or `p=reject` once
   DKIM/SPF pass), per the plan's "SPF/DKIM/DMARC strict" requirement.
4. Wait for Postmark to report the domain **verified**.

`EMAIL_FROM` must be an address on that verified domain (default
`notifications@soteria.io`).

### 3. Configure the bounce / complaint webhook

Postmark webhooks carry no signature, so the URL embeds a secret token.

1. Generate a token: `openssl rand -hex 32` → this is `POSTMARK_WEBHOOK_TOKEN`.
2. In the server → **Webhooks** → add a webhook:
   - URL: `https://YOUR_APP_HOST/webhooks/postmark/THE_TOKEN`
   - Enable the **Bounce** and **Spam Complaint** events.
3. If `POSTMARK_WEBHOOK_TOKEN` is unset the route returns 404 — set it before
   adding the webhook.

### 4. Environment variables

| Variable | Required | Value |
|---|---|---|
| `POSTMARK_TOKEN` | yes | Server API Token from step 1 |
| `EMAIL_FROM` | recommended | Verified `From` address (default `notifications@soteria.io`) |
| `POSTMARK_WEBHOOK_TOKEN` | recommended | Random secret from step 3 |

## Verify

1. Restart the server. The startup log should read `email enabled (postmark)`
   instead of `email disabled (no POSTMARK_TOKEN) — using log mailer`.
2. Trigger a real send — e.g. invite a teammate — and confirm it lands, and
   appears in Postmark's **Activity** stream.
3. Test the webhook: send to Postmark's bounce tester
   (`test@bounce-testing.postmarkapp.com`) or use Postmark's "send test" on
   the webhook. Confirm the address appears in the `email_suppressions`
   table and the log shows `email address suppressed via postmark webhook`.
4. A later send to a suppressed address should be skipped with
   `email skipped: recipient suppressed`.
