# AWS RDS / Aurora PostgreSQL → Vesta

Goal: produce a logical `pg_dump` of your RDS (or Aurora) PostgreSQL
database, then drill it in Vesta for signed evidence.

> RDS automated **snapshots** are physical, not logical dumps — Vesta drills
> logical `pg_dump` output today. The simplest path is a `pg_dump` against
> the instance (or, to avoid load on primary, against a read replica).

## 1. Export the dump

You need the instance endpoint, a database user, and the database name. RDS
requires TLS, so set `PGSSLMODE=require`.

```sh
export PGSSLMODE=require
pg_dump \
  -h mydb.abc123.us-east-1.rds.amazonaws.com \
  -p 5432 \
  -U app_user \
  -d appdb \
  -Fc \
  -f appdb.dump
```

- `-Fc` is the custom format Vesta restores (compressed, selective).
- To keep load off your primary, point `-h` at a **read replica** endpoint.
- For very large databases, run this from an EC2 box in the same region so
  the dump doesn't traverse the internet.

Confirm you got a real dump:

```sh
pg_restore --list appdb.dump | head   # should print the archive's TOC
```

## 2. Drill it in Vesta

Follow the common path in [README.md](README.md#from-a-dump-to-signed-evidence-common-to-every-guide):
hand the dump to Vesta, add the database, add a `table_exists` assertion on
a table that must survive (e.g. `public.users`), run the drill, schedule it
weekly, and download + verify the signed PDF.

## Notes

- **IAM auth:** if your RDS uses IAM database authentication, generate a
  token first (`aws rds generate-db-auth-token …`) and pass it as the
  password for the `pg_dump` connection.
- **Extensions:** if your app relies on extensions (e.g. `postgis`,
  `pg_trgm`), a drill is exactly what catches a restore that fails because an
  extension isn't present in the sandbox — that's a finding worth having.
