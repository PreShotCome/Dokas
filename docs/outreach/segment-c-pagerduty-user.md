# Segment C — Already uses PagerDuty

**Trigger:** PagerDuty is on their public status page ("Incident management
by PagerDuty"), listed in a job description, or their JS embeds contain
`pdt-*` — PublicWWW is your friend. Any SRE / platform-team hire posts
usually name it.

**Why this works:** The buyer has already conceded that on-call matters and
they pay for tooling that pages a human. Dokaz slots into that muscle memory
— a drill failure fires PagerDuty natively, no glue code. And the value
prop is a small extension of what they already believe: "shift left on
recovery."

---

## Email 1

**Subject:** PagerDuty for restore failures at {{company}}

Hi {{first_name}},

If a database restore breaks at {{company}} today, when do you find out?
Usually the answer is "when we need it" — because the backup job's success
log doesn't measure whether the archive actually restores.

We run a real restore in an isolated sandbox on a cadence you set. If it
fails, we page you through PagerDuty using the routing key you already
have — no new tool to learn, no new dashboard.

The signed PDF each drill produces is also what your auditor / insurer
wants for their restore-tested evidence checklist, so it pays for itself
against the compliance side even before the on-call side.

Want to see a real signed report? You can verify one in your browser at
dokaz.net/verify without signing up.

Ian
Dokaz — dokaz.net

---

## Email 2 (send 4 business days later, no reply)

**Subject:** re: PagerDuty at {{company}}

{{first_name}} — one more angle.

The dedup key on the PagerDuty side is per-database, so a chronically-broken
backup only pages once until it recovers. And drill-recovered fires
`resolve` automatically. Same shape as the check-in monitors you already
have.

If it's not a fit, no offence — I'll drop off.

Ian
