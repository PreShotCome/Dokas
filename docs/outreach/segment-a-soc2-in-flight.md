# Segment A — SOC 2 audit in flight

**Trigger:** Publicly hiring "SOC 2 audit lead", "compliance engineer with
SOC 2 experience", or has a Vanta/Drata trust page showing SOC 2 Type I or
Type II underway (badge without a completion date).

**Why this segment first:** The auditor has already asked them for
restore-tested-in-last-12-months evidence, and they're scrambling. Highest
buying intent in the funnel. Response rate here should be 8–12%.

---

## Email 1

**Subject:** Restore evidence for {{company}}'s SOC 2

Hi {{first_name}},

Saw {{company}} is finalising SOC 2 — congratulations, hardest part of a
seed-stage security program.

CC7.4 and A1.3 usually catch people off-guard: the auditor asks for a
*restored* backup with data checks, not the backup job's success log.
Most teams end up screenshotting a manual test-restore the night before
the walkthrough.

We run that restore automatically — pg_restore into an isolated sandbox,
your assertions, signed PDF. If you want to see one before we exchange a
word, verify a live signed report in your browser:
dokaz.net/verify.

Would that solve a real problem for your audit? Reply yes and I'll send
the sample PDF + signature you can verify.

Ian
Dokaz — dokaz.net

---

## Email 2 (send 4 business days later, no reply)

**Subject:** re: {{company}}'s SOC 2 evidence

{{first_name}} — one more thought.

The evidence CC7.4 asks for is durable: once you have a signed drill
report, you can hand the same PDF to your ISO 27001 audit next year and
to the cyber-insurance renewal after that. It's the artifact the whole
regulated-B2B stack is converging on.

If it's the wrong month to be thinking about it, no offence taken — I'll
step out of your inbox.

Ian
