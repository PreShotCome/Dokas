# Neon → Vesta

Goal: produce a logical `pg_dump` of your Neon database, then drill it in
Vesta for signed evidence.

## 1. Get your connection string

In the Neon console: **your project → Connection Details**. Copy the
connection string. Two things matter for `pg_dump`:

- Use the **direct** (unpooled) connection, **not** the pooler endpoint
  (the host *without* `-pooler`). `pg_dump` opens a session the pooler
  doesn't support well.
- Neon requires TLS — the string already carries `?sslmode=require`.

## 2. Export the dump

```sh
export NEON_URL="postgresql://user:password@ep-cool-darkness-123456.us-east-2.aws.neon.tech/neondb?sslmode=require"
pg_dump "$NEON_URL" -Fc -f neondb.dump
```

- `-Fc` is the custom format Vesta restores.
- If you have multiple Neon **branches**, dump the branch you actually
  recover from (usually `main`/`production`).

Confirm it's a real dump:

```sh
pg_restore --list neondb.dump | head
```

## 3. Drill it in Vesta

Follow the common path in [README.md](README.md#from-a-dump-to-signed-evidence-common-to-every-guide):
hand the dump to Vesta, add the database, add a `table_exists` assertion on
a critical table, run the drill, schedule it weekly, and download + verify
the signed PDF.

## Notes

- **Automate it:** run the `pg_dump` above as a scheduled job (GitHub
  Actions, a cron box, a Neon scheduled task) so a fresh dump is always
  available for the next drill.
- **Version match:** Neon tracks recent PostgreSQL majors — make sure your
  local `pg_dump --version` is `>=` your Neon Postgres version.
