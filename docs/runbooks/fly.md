# Fly Machines — per-drill sandbox

## What it powers

The drill sandbox. By default a drill restores into a temporary database on
the app's own Postgres host (the local runner — fine for dev/CI). The Fly
runner instead provisions a **dedicated, ephemeral Fly Machine** per drill —
a throwaway Postgres VM, isolated from the app and from other drills, and
destroyed at teardown. This is the "sandbox isolation" the plan calls the
security keystone.

## Code status — implemented, build-verified only

- `internal/fly` — a Fly Machines API client (create / wait / destroy),
  unit-tested against a fake API server.
- `internal/runner/FlyMachineRunner` — `Provision` creates a Postgres
  machine and waits for it to start; `Teardown` destroys it; `Restore`
  loads the dump by running `pg_restore`/`psql` from the app against the
  machine's private DSN (identical to the local runner — only the sandbox
  lifecycle differs).
- **It has not been exercised against the live Fly API** — verified by
  build + unit tests only. Treat the first real drill as the integration
  test.

### Architecture / prerequisite

`Restore` and the assert step reach the machine's Postgres at
`<machine-id>.vm.<app>.internal:5432` over Fly's private network (6PN). That
DNS resolves **only when the app itself runs on Fly in the same
organization** as the drill app. Running drills on Fly Machines therefore
requires the app to be deployed on Fly.

## Setup

1. Install `flyctl` and `fly auth login`.
2. Create a Fly app to hold drill machines: `fly apps create soteria-drills`.
3. Mint an API token: `fly tokens create deploy -a soteria-drills`
   (or an org token) — this is `FLY_API_TOKEN`.
4. Choose a `FLY_SANDBOX_DB_PASSWORD` (a random string; the sandbox
   databases are ephemeral and private, but pick a strong value anyway).

### Environment variables

Both `FLY_API_TOKEN` and `FLY_APP_NAME` must be set to switch on the Fly
runner; otherwise the local runner is used.

| Variable | Required | Value |
|---|---|---|
| `FLY_API_TOKEN` | yes | Fly API token |
| `FLY_APP_NAME` | yes | The app that hosts drill machines |
| `FLY_SANDBOX_DB_PASSWORD` | yes | Password for each sandbox's Postgres |
| `FLY_POSTGRES_IMAGE` | no | Postgres image (default `postgres:16-alpine`) |
| `FLY_REGION` | no | Fly region (default `iad`) |

## Verify

1. Restart; the log should read `drill runner: fly machines`.
2. Start a drill. While it runs, `fly machines list -a <FLY_APP_NAME>`
   should show a machine appear, then disappear at teardown.
3. The drill should reach the same succeeded/failed verdict as on the local
   runner, with the evidence PDF produced.

If a drill hangs at the fetch/restore step, the app most likely cannot reach
the machine over 6PN — confirm the app and the drill app share a Fly org and
private network.
