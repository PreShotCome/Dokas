# Security Policy

Dokaz is a backup-verification product: customers trust it to tell the
truth about whether their data can be restored, and to produce evidence
that holds up under scrutiny. A vulnerability in Dokaz is therefore not
just a bug — it can undermine the integrity of evidence other people
rely on. We treat security reports accordingly, and we want to make it
easy and safe for researchers to tell us about problems.

## Reporting a vulnerability

Email **security@dokaz.io** with the details. Please include:

- a description of the issue and its impact,
- the steps, proof-of-concept, or request/response needed to reproduce it,
- any affected URLs, accounts, or versions, and
- how you'd like to be credited (or that you'd prefer to stay anonymous).

If you need to send sensitive material, say so in your first email and
we'll arrange an encrypted channel.

> **Note (pre-launch):** the `security@dokaz.io` mailbox goes live once
> the Postmark sender domain for `dokaz.io` is verified (launch-readiness
> item #4). Until then, route reports through the repository owner.

Please **do not** open a public GitHub issue, pull request, or
discussion for a security problem, and please don't disclose it publicly
until we've had a chance to fix it and agree on timing with you.

## Our commitment to you

When you report in good faith, we commit to the following timeline,
measured from when your report reaches us:

| Stage | Target | What it means |
|---|---|---|
| **Acknowledgement** | within **24 hours** | A human confirms we received your report and have started looking. |
| **Triage** | within **72 hours** | We confirm or refute the issue, assess severity and impact, and tell you what we found and what we plan to do. |
| **Fix plan** | within **7 days** | We share a remediation plan with target dates. Critical issues are worked immediately; lower-severity issues get a scheduled fix with a date we'll commit to. |

We'll keep you updated as we work, and we'll let you know when the fix
ships. If we ever need longer — some fixes are genuinely hard — we'll
say so explicitly rather than going quiet.

## Safe harbour

We will not pursue or support legal action against anyone who, in good
faith, follows this policy while researching and reporting a
vulnerability. Specifically, as long as you:

- make a good-faith effort to avoid privacy violations, data
  destruction, service degradation, and interruption to others;
- only access, modify, or store data that belongs to you (use your own
  test accounts — never another customer's data);
- do not run denial-of-service, spam, social-engineering, or physical
  attacks against Dokaz, its staff, or its infrastructure;
- give us a reasonable amount of time to fix the issue before any public
  disclosure, and coordinate timing with us;

then we consider your research authorised, we will not treat it as a
violation of our terms of service, and we will not report it to law
enforcement or pursue a claim under anti-hacking statutes. If a third
party brings action against you for activity that complied with this
policy, we'll make it known that your actions were authorised.

This safe harbour does not extend to accessing other customers' data,
degrading the service for others, or exfiltrating data beyond the
minimum needed to demonstrate a vulnerability.

## Scope

In scope:

- the Dokaz web application and its API (`/v1`),
- the evidence-signing and verification machinery,
- authentication, session, billing, and tenant-isolation logic.

Out of scope (report-only, generally not eligible for acknowledgement):

- findings that require a compromised device, a malicious browser
  extension, or physical access to a logged-in session;
- volumetric denial-of-service and rate-limit exhaustion;
- reports from automated scanners with no demonstrated impact;
- missing security headers or cookie flags with no concrete exploit;
- social engineering of Dokaz staff or customers.

## Researcher acknowledgements

We're grateful to the people who help keep Dokaz and its customers
safe. With your permission, we credit researchers who report valid
vulnerabilities here, once a fix has shipped.

<!-- Add entries as: name / handle — short description — month YYYY -->

*No reports yet — this list starts empty by design. If you're reading
this, you could be first.*
