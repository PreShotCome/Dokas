# Runbook: customer drill-failure response

**Audience:** whoever is on call for Selket.
**Trigger:** a customer reports — or our own monitoring detects — that a
drill failed, produced a wrong verdict, or that a customer doubts a
verdict Selket gave them.

This is the most reputationally dangerous class of incident Selket has.
Selket exists to be trusted about whether a backup restores. A drill that
fails silently, fails spuriously, or — worst of all — reports PASSED when
the restore was actually broken, attacks the one thing the product sells.
Treat every one of these as a potential integrity incident until proven
otherwise.

---

## 1. Severity: P1 vs P2

Promote to **P1** (all-hands, page immediately, customer comms within the
hour) if **any** of these is true:

- A drill reported **PASSED for a restore that did not actually work** —
  a false-positive verdict. This is the single worst failure mode and is
  always P1, even for one customer.
- The signed evidence PDF **does not verify** against the published key,
  or the verdict in the PDF disagrees with the verdict in the app.
- Failures span **multiple customers** at once (a platform regression,
  not a single-tenant data issue).
- A customer is **inside an audit window** and the failure blocks them
  producing evidence they need on a deadline (see §5).
- Tenant isolation is implicated — one customer's drill touched another
  customer's data, sandbox, or evidence.

Everything else is **P2** (handle in business hours, comms within the
day):

- A single customer's drill fails for a cause local to their data or
  source (bad credentials, unreachable source, a genuinely failing
  assertion that *correctly* reports their backup is broken).
- A spurious failure (false-negative) that did not block an audit
  deadline.

A correct FAILED verdict — Selket telling a customer their backup is
broken because it genuinely is — is **not an incident**. It's the product
working. Confirm that's what happened before escalating; then help the
customer fix their backup.

---

## 2. Timeline

Clock starts when the report reaches us (customer email, alert, or
support ticket).

| By | Do |
|---|---|
| **+5 min** | Acknowledge. Page the on-call for P1. Open an incident channel and start a timeline doc. Pull the `drill_id`, account, and the failing step. |
| **+30 min** | Triage to a root cause *class* using the table in §4. Decide P1 vs P2. For P1, post the first customer holding message (§6). Identify blast radius: one tenant or many? |
| **+120 min** | Have either a fix in flight or a concrete plan with an ETA. For P1, send the customer a substantive update (what we found, what we're doing). |
| **+24 h** | Resolution or a credible workaround in the customer's hands. If a false-positive verdict ever shipped, the affected PDFs are identified and the customers are notified directly. |
| **+7 d** | Written postmortem published internally, with the new e2e-smoke check (see §7) merged and green. No postmortem is "done" without that check. |

---

## 3. First moves (any severity)

1. **Reproduce against the verdict, not the symptom.** Find the drill in
   the DB and read its `drill_steps` rows: which step has
   `status = 'failed'`, and what's in its failure reason?
   The pipeline is **provision → fetch → restore → assert → report →
   teardown**; the failing step narrows the cause fast.
2. **Check the evidence.** If a PDF was produced, verify its signature
   against `/.well-known/evidence-signing-keys.pem` and confirm the
   verdict text matches the drill's recorded status. A mismatch here is
   an automatic P1.
3. **Decide false-positive / false-negative / correct.** Did the restore
   actually work? If yes and we said FAILED → false-negative (spurious).
   If no and we said PASSED → false-positive (P1). If the restore matches
   the verdict → correct; help the customer, no incident.
4. **Scope the blast radius** before fixing: query for other drills that
   hit the same step/assertion/error in the same window.

---

## 4. Known-failure-mode triage table

Map the failing step to its likely causes. The right-hand column is the
class of root cause to confirm or rule out — including the five seam bugs
that bit us in one onboarding session and that the e2e-smoke harness now
guards.

| Failing step | What it does | Likely causes to check |
|---|---|---|
| **provision** | Spins up the sandbox DB | Sandbox compute unavailable (Fly Machines quota/outage), ephemeral signing/encryption key on restart breaking later decrypt, DB pool exhaustion. |
| **fetch** | Pulls the customer's backup artifact | Source unreachable, bad/expired credentials, object-store (S3/R2) permissions, dump larger than sandbox disk. *Customer-cause is common and is P2.* |
| **restore** | Loads the dump into the sandbox | Dump format/version mismatch (`pg_dump` vs server), truncated/corrupt artifact (this is a *correct* FAILED — their backup is broken), restore timeout. |
| **assert** | Runs configured assertions | A `row_count` / `table_exists` / `column_exists` / `no_nulls` / `sql_query` assertion failing because the **backup is genuinely missing data** (correct FAILED — the product working), **vs** a schema/code drift between the SELECT and `rows.Scan`, **vs** a new assertion enum value not added to the DB CHECK constraint (insert silently rejected). The last two are *our* bugs. |
| **report** | Renders + signs the evidence PDF | PDF stamped FAILED for a passing drill (in-memory status drift), mojibake from a non-Latin-1 UTF-8 char (em-dash, smart quotes) in the PDF font, wrong/empty signature, `absoluteURL` link recursion producing a bad evidence URL. |
| **teardown** | Tears down the sandbox | Usually cosmetic — the verdict is already decided. Leaked sandboxes are a cost/hygiene issue, not an evidence-integrity one. Still file it. |

If the failing step is **assert** or **restore**, your first job is to
decide whether Selket is *correctly* reporting a broken backup. If it is,
the customer's backup is the problem and Selket did its job — pivot to
helping them. Only if the restore genuinely worked is this our bug.

---

## 5. Audit-window escalation

If the customer flags they're inside an audit, examiner request, or
regulatory deadline, **escalate to P1 regardless of the technical
severity** and:

- Assign a named owner whose only job is that customer until resolved.
- Give the customer a direct human contact and an explicit ETA.
- If we can't produce a fresh valid verdict in time, help them present
  what they have: the prior passing evidence (which remains
  independently verifiable via the published key) plus a signed
  statement of the in-progress incident and timeline.
- Never let an audit-window customer wait on the default comms cadence.

---

## 6. Customer-comms templates

**Holding message (P1, within the hour):**

> Subject: Selket — investigating your drill on {target}
>
> Hi {name},
>
> We've seen the issue with your drill ({drill_id}) and a Selket engineer
> is actively investigating. We treat anything touching the correctness
> of a verdict as our highest priority. I'll send a substantive update by
> {time}. If you're working against an audit or other deadline, reply and
> tell me the date — we'll prioritise accordingly.
>
> — {name}, Selket

**Substantive update (+120 min):**

> We've traced this to {root-cause class}. It {does / does not} affect the
> correctness of any verdict already issued to you. {What we're doing} and
> we expect {resolution / workaround} by {ETA}. Your previously issued
> evidence remains valid and independently verifiable.

**False-positive disclosure (only if a wrong PASSED ever shipped):**

> We need to be direct with you: a drill we reported as PASSED on {date}
> did not, on review, reflect a working restore. Here is exactly what
> happened, which evidence is affected, and what we've done so it can't
> recur. We're re-running the drill now and will issue corrected evidence.

Honesty is the policy. A product whose value is trustworthy evidence does
not get to spin a false verdict.

---

## 7. Standing rule: every prod-origin failure adds an e2e-smoke check

**Any drill failure that originated in our code in production must add a
new check to the `e2e-smoke` harness (`cmd/e2e-smoke`) before its
postmortem is considered complete.**

The harness walks the full signup → connect → assert → run drill →
download PDF → verify flow and exists precisely because the seam bugs
that hurt us each passed their own unit tests — the damage was always in
the gaps between components. A real production failure is proof we have a
blind spot the harness doesn't cover. Closing that gap, in CI, on every
push, is the deliverable of the incident — not an optional follow-up.

The postmortem template's "action items" section therefore has one
mandatory row: *"e2e-smoke check added (PR #…, merged, green)."* If that
row is empty, the incident is not closed.
