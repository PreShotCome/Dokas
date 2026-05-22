# Physical PostgreSQL backups

## Status — seam (documented, not implemented)

Soteria verifies all four **logical** `pg_dump` formats today (plain
SQL, custom, tar, directory — see `docs/backlog.md` → Resolved).
**Physical** backups — `pg_basebackup` base backups, pgBackRest, WAL-G —
are deliberately not implemented yet: they need a different restore model
than the one the runner is built around, and a half-built version would be
worse than an honest seam. This runbook documents the seam, why physical
restore belongs on a different runner, and a bounded plan to build it.

## What it powers

A physical backup is a byte-level copy of a whole PostgreSQL **cluster**
(the `$PGDATA` directory + the WAL needed to reach consistency). It is how
most production Postgres is actually backed up — base backups plus
continuous WAL archiving — so verifying physical backups is core to the
product, not a nice-to-have. The backlog calls dump-format coverage "the
project killer per red-team"; physical formats are the half of that not
yet covered.

## Why it is a different restore model

The logical path is **database-centric**. `internal/runner` provisions a
sandbox, `Restore` loads a dump into it with `psql`/`pg_restore`, and the
assert step dials `Sandbox.DSN`. Everything hangs off "a database you can
connect to."

A physical backup is not a dump you load into a database — it **is** a
cluster. Verifying it means laying the data directory down, starting a
PostgreSQL server process on it, letting it run crash/archive recovery to
a consistent state, and only then connecting. That is whole-cluster
restore, and it does not fit "load into a temp database":

- The `LocalRunner` puts each drill's sandbox in a temp database **on the
  app's own Postgres host** (`CREATE DATABASE`). It has nowhere to stand up
  a *second*, independent cluster — and `postgres` will not run as root,
  refuses a non-`0700` data directory, and needs the server binaries
  (`postgres`, `pg_ctl`, `initdb`), which are not part of the
  `postgresql-client` package CI installs.
- pgBackRest and WAL-G add their own restore tooling and a repository
  (object storage + a stanza config) on top of that.

## Where it belongs — the Fly runner

The `FlyMachineRunner` already provisions a **dedicated, ephemeral Postgres
VM per drill** (see `docs/runbooks/fly.md`). That is the natural home for
physical restore: the VM's own cluster is the thing being restored.

- `Provision` boots a blank Fly Machine.
- `Restore` (physical) copies the base backup onto the machine, lays it
  down as `$PGDATA`, drops in a `recovery`/`standby` signal so Postgres
  replays WAL on start, and starts the server — all *inside* the VM, where
  Postgres runs as the `postgres` user with a private data directory.
- The assert step dials the machine's private DSN exactly as it does for a
  logical drill — the sandbox contract (`Sandbox.DSN`) is unchanged.

So physical-backup support is gated on the Fly runner, which is itself
implemented but **build-verified only** (not yet exercised against the live
Fly API). Building physical restore against the local runner would mean
fighting the shared-host model; building it on Fly is the right design but
inherits Fly's "not yet live-verified" status.

## How to implement it (bounded follow-up)

1. **Source kind.** Add `postgres_basebackup` to the `source_kind` CHECK on
   `database_targets` (a migration) and a matching option in the target
   create form + `/v1` API. Keep pgBackRest / WAL-G as later kinds.
2. **Runner contract.** Give `Sandbox` a `Kind` field and make `Provision`
   kind-aware; `Restore`/`Teardown` branch on it. `Rehydrate` already
   rebuilds the handle from the persisted sandbox name — keep the name
   kind-tagged so it stays a pure reconstruction.
3. **Fly physical restore.** In `FlyMachineRunner.Restore`, when the kind is
   physical: fetch the base-backup archive, extract it as the machine's
   `$PGDATA`, write the recovery signal, start Postgres, and wait for it to
   accept connections after recovery.
4. **Fixtures + CI.** Add a tiny base-backup fixture and a round-trip test.
   Running it needs the Postgres **server** package (for `pg_basebackup` +
   `postgres`); add it to CI, or keep the physical test behind a build tag
   that the Fly integration job runs.
5. **pgBackRest / WAL-G.** Each is a further `source_kind` whose `Restore`
   shells out to that tool's `restore` against a configured repository —
   additive once the base-backup path exists.

Until then, logical `pg_dump` coverage is genuinely complete and a sound
interim: the large majority of small/medium Postgres backups are logical
dumps, and those are fully drilled today.
