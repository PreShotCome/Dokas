# Setup guides

These guides get you from a managed PostgreSQL backup to a signed
Proof-of-Recovery PDF you can hand an auditor or an insurer. Each one covers
the provider-specific part — exporting a correct dump — then follows the same
short path to evidence.

| Your Postgres lives on | Guide |
|---|---|
| AWS RDS / Aurora PostgreSQL | [aws-rds-postgres.md](aws-rds-postgres.md) |
| Neon | [neon-postgres.md](neon-postgres.md) |
| Supabase | [supabase-postgres.md](supabase-postgres.md) |

Any standard `pg_dump` works — RDS, Neon, Supabase, Render, Railway, or a
plain VPS. The guides above are the most common stacks; the export step is
the only part that differs.

## From a dump to signed evidence (common to every guide)

Once you have a `pg_dump` file, the rest is the same:

1. **Hand the dump to Vesta.** Today Vesta drills a dump from a path it can
   reach; during onboarding we set up that location with you (a synced
   backups directory). *Pulling dumps directly from S3 / R2 is on the
   roadmap — ask us where it stands.*
2. **Add the database.** In the app: **Databases → Add database**, name it,
   and point it at the dump.
3. **Add an assertion.** Start with `table_exists` on a table you know must
   survive a restore (e.g. `public.users`). Add `row_count` (">= N") and
   `no_nulls` on critical columns as you go; `sql_query` lets you write your
   own read-only check and capture the rows into evidence.
4. **Run a drill.** Watch provision → fetch → restore → assert → report →
   teardown. A pass means the backup restored cleanly *and* every assertion
   held.
5. **Schedule it.** Set a cadence (weekly is plenty for most). Cyber-insurance
   renewals commonly want proof of a restore within the last 90 days — a
   weekly drill clears that with room to spare.
6. **Download + verify the evidence.** Grab the Proof-of-Recovery PDF. Verify
   it independently with `vesta-verify` against the published key — see
   [../sample-evidence/README.md](../sample-evidence/README.md).

## Two things that bite everyone

- **Match `pg_dump` to the server version.** Run a `pg_dump` whose version is
  **>=** your server's major version, or the restore can fail on newer
  syntax. `pg_dump --version` should be at least your server's major.
- **On Windows, never write a dump with `>`.** PowerShell's `>` re-encodes
  the stream as UTF-16 and corrupts the binary dump. Use `pg_dump -f file`
  (as below) or `Invoke-WebRequest -OutFile`, never `... > file`.
