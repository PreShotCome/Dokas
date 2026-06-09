# Selket onboarding: your first drill in 7 steps

This walkthrough takes you from a fresh account to an independently
verified piece of evidence — without needing your own database. You'll
drill a tiny sample backup Selket ships, watch every step of the
pipeline run, download the signed PDF, and verify its signature with a
standalone tool that contains none of Selket's code.

If you can complete this, you've exercised the whole product:
**provision → fetch → restore → assert → report → teardown**, plus
independent verification.

Throughout, `https://app.selket.io` stands in for your Selket instance
(use `http://localhost:5173` if you're running locally).

---

## Step 1 — Sign up

Go to `https://app.selket.io/signup`, create an account, and verify your
email. New accounts get a 14-day trial — enough to run drills
immediately, no card required. After signing in you land on the
dashboard.

## Step 2 — Download the sample backup

Selket hosts a known-good fixture: a tiny PostgreSQL custom-format dump
with a single `public.events` table. Download it:

**macOS / Linux:**

```sh
curl -fL https://app.selket.io/onboarding/sample.dump -o sample.dump
```

**Windows (PowerShell):**

```powershell
Invoke-WebRequest https://app.selket.io/onboarding/sample.dump -OutFile sample.dump
```

> ⚠️ **On Windows, use `Invoke-WebRequest -OutFile`, not `... > sample.dump`.**
> PowerShell's `>` redirection re-encodes the stream as UTF-16 text and
> corrupts the binary dump — the restore will then fail for a reason that
> has nothing to do with Selket. The same caution applies to piping `curl`
> output through `>` in older PowerShell. Always write binaries with
> `-OutFile` (or `curl -o`).

Confirm you got a real dump, not an HTML error page:

```sh
file sample.dump   # → PostgreSQL custom database dump
```

## Step 3 — Connect the sample as a source

In the app, go to **Databases → Add database**. Give it a name
(`Sample`), choose the upload/file source, and provide `sample.dump`.
This registers a *target* — the thing drills run against. You don't need
real database credentials for this fixture; you're handing Selket the
dump directly.

## Step 4 — Add a `table_exists` assertion

A drill is only as meaningful as what it checks. On the target you just
created, add an assertion:

- **Kind:** `table_exists`
- **Table:** `events` (schema `public`)

This asserts that, after the backup is restored into a clean sandbox, the
`public.events` table is actually present. That's the core question a
backup drill answers: *did the data come back?* (The other assertion
kinds — `row_count`, `column_exists`, `no_nulls`, `sql_query` — let you
check the data is not just present but correct; start with
`table_exists`.)

## Step 5 — Run the drill

Hit **Run drill**. Watch the step list advance:

1. **provision** — a clean throwaway sandbox database is created.
2. **fetch** — your `sample.dump` is pulled into the sandbox environment.
3. **restore** — the dump is restored into the sandbox.
4. **assert** — your `table_exists` assertion runs against the restored DB.
5. **report** — a PDF is rendered and signed with Selket's evidence key.
6. **teardown** — the sandbox is destroyed; nothing lingers.

When it finishes you'll see **Verdict: PASSED**. That verdict means the
backup restored cleanly *and* every assertion held.

## Step 6 — Download the evidence PDF

Open the drill and download the **evidence PDF**. It records the target,
the steps, each assertion's expected vs. actual result, the verdict, and
a signature fingerprint. Also download the **signature JSON** (the
detached signature) — you'll need both to verify.

Via the API, the same two artifacts are at:

```
GET /v1/drills/{id}/evidence    # the PDF
GET /v1/drills/{id}/signature   # the detached signature JSON
```

## Step 7 — Verify the signature independently

This is the step that makes Selket's evidence worth anything: you can
prove the PDF is genuine and untampered **without trusting Selket's own
app**.

Fetch Selket's public signing keys:

```sh
curl -fL https://app.selket.io/.well-known/evidence-signing-keys.pem -o selket.pem
```

Each key block in that file is preceded by a `# PublicKeyID:` comment.
Match the `public_key_id` in your signature JSON to one of them. Then run
the standalone verifier (`selket-verify`, a stdlib-only Go program — see
launch-readiness item #9 for the released binaries, or build it from
`cmd/selket-verify`):

```sh
selket-verify --pdf=evidence.pdf --sig=signature.json --pubkey=selket.pem
```

Exit code `0` and `signature is valid` means: this exact PDF was signed
by the key Selket publishes, and not a byte has changed since. That's
your independently verifiable proof the drill happened and passed.

---

## Troubleshooting

These are the failure modes most likely to bite a first-time user. The
first one is on you; the rest are bugs we've fixed and now guard against
in CI with the `e2e-smoke` harness — if you hit one, it's worth reporting.

| Symptom | Likely cause | Fix |
|---|---|---|
| **restore** fails on a dump that should be fine | The dump was corrupted on download — almost always PowerShell `>` re-encoding it as UTF-16. | Re-download with `Invoke-WebRequest -OutFile` / `curl -o`. Check `file sample.dump` says *PostgreSQL custom database dump*. |
| Drill page shows a server error right after **assert** | Schema/code drift — a `SELECT` and its `rows.Scan` disagree on columns. | Our bug. The e2e-smoke harness now covers this seam; report the `drill_id`. |
| Your assertion silently never takes effect / insert rejected | A new assertion enum value not added to the database `CHECK` constraint. | Our bug — migrations and enum values must move together. Report it. |
| PDF says **FAILED** but the drill clearly **PASSED** (or vice-versa) | In-memory status drift between the drill record and the rendered PDF. | Our bug, and a serious one — report it immediately with the `drill_id`. |
| PDF text shows garbled characters (e.g. `â€"` where a dash should be) | Mojibake: a UTF-8 character (em-dash, smart quote) rendered in a Latin-1 PDF font. | Our bug; the report renderer must handle non-Latin-1 input. Report it. |
| `selket-verify` says invalid, but the PDF looks right | Wrong public key, or the PDF/signature was modified after download (even re-saving can do it). | Re-download all three files fresh; match the `public_key_id` to the right key block in `selket.pem`. |

If a verdict is ever wrong in either direction, that's our most serious
class of bug — see `docs/runbooks/customer-drill-failure-response.md` —
and we want to hear about it right away.
