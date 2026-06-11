# Supabase → Vesta

Goal: produce a logical `pg_dump` of your Supabase database, then drill it in
Vesta for signed evidence.

## 1. Get your connection string

In the Supabase dashboard: **Project Settings → Database → Connection
string**. For `pg_dump` use the **direct / session** connection on port
**5432** — not the transaction pooler on port **6543** (the pooler doesn't
support the session `pg_dump` needs).

## 2. Export the dump

### Option A — `pg_dump` directly

```sh
export SUPABASE_URL="postgresql://postgres:[YOUR-PASSWORD]@db.abcdefgh.supabase.co:5432/postgres?sslmode=require"
pg_dump "$SUPABASE_URL" -Fc -f supabase.dump
```

### Option B — the Supabase CLI

```sh
supabase db dump --db-url "$SUPABASE_URL" -f supabase.sql
```

`pg_dump -Fc` (Option A) gives the compressed custom format Vesta restores;
the CLI's plain SQL also drills fine. Use whichever fits your pipeline.

Confirm it's a real dump (Option A):

```sh
pg_restore --list supabase.dump | head
```

## 3. Drill it in Vesta

Follow the common path in [README.md](README.md#from-a-dump-to-signed-evidence-common-to-every-guide):
hand the dump to Vesta, add the database, add a `table_exists` assertion on
a table that must survive a restore, run the drill, schedule it weekly, and
download + verify the signed PDF.

## Notes

- **Auth schema:** Supabase keeps users in `auth.users`. A great assertion is
  `table_exists` on `auth.users` plus a `row_count` (">= 1") — proof your
  user accounts actually come back, not just your app tables.
- **Roles/policies:** RLS policies and roles are part of the dump; a clean
  restore in the drill confirms they reload without error.
