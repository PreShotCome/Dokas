# Google + GitHub OAuth — social login

## What it powers

"Continue with Google" / "Continue with GitHub" on the sign-in page. The
provider returns the user's verified email; an existing account signs in,
and a first-time email gets a user + personal account provisioned
automatically (email-verified, since the provider vouched for it). MFA still
applies — OAuth replaces the password, not the second factor.

## Code status — complete

- `internal/oauth` — the OAuth 2.0 authorization-code flow for both
  providers, talking to their REST endpoints directly (no SDK).
- `GET /auth/{provider}/start` and `/auth/{provider}/callback` — CSRF-state
  protected; the callback finds-or-creates the user and starts the session.
- Each provider activates only when both its client ID and secret are set;
  the login page renders a button per active provider.

## Setup

### Google

1. In the **Google Cloud Console**, create (or pick) a project.
2. **APIs & Services → OAuth consent screen** — configure it (External; add
   the app name, support email, and the `…/auth/userinfo.email` scope).
3. **APIs & Services → Credentials → Create credentials → OAuth client ID**:
   - Application type: **Web application**.
   - **Authorized redirect URI**: `https://YOUR_APP_HOST/auth/google/callback`
     (must match exactly).
4. Copy the **Client ID** and **Client secret**.

### GitHub

1. **GitHub → Settings → Developer settings → OAuth Apps → New OAuth App**.
2. **Authorization callback URL**: `https://YOUR_APP_HOST/auth/github/callback`.
3. Copy the **Client ID**, then **Generate a new client secret** and copy it.

### Environment variables

A provider turns on only when *both* of its variables are set.

| Variable | Value |
|---|---|
| `GOOGLE_OAUTH_CLIENT_ID` | Google OAuth client ID |
| `GOOGLE_OAUTH_CLIENT_SECRET` | Google OAuth client secret |
| `GITHUB_OAUTH_CLIENT_ID` | GitHub OAuth client ID |
| `GITHUB_OAUTH_CLIENT_SECRET` | GitHub OAuth client secret |

## Staff SSO step-up

When **Google** OAuth is configured, it also gates the staff admin panel:
reaching `/admin/*` requires a staff user to re-prove identity via Google
within the last hour (a "step-up"). The step-up re-checks the live
`STAFF_EMAILS` allowlist, so removing someone from it revokes their admin
access at the next step-up. With Google OAuth unset (dev / CI), no step-up
is required and `is_staff` admits directly — nothing extra to configure.

## Verify

1. Restart; the startup log should read `social login enabled` with the
   provider list.
2. The `/login` page now shows a "Continue with …" button per provider.
3. Click one → authorize on the provider → you land on `/dashboard` signed
   in. A brand-new email gets an account created; an existing email signs
   into its account.
4. If the callback errors with a redirect-URI mismatch, the URL registered
   with the provider does not exactly match `…/auth/<provider>/callback`.
