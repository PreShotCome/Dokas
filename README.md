# Restore Drill

Backup verification as a service. We periodically restore your database
dumps in an isolated sandbox, run assertions, and produce auditor-grade
evidence that your backups are actually restorable.

This repo contains the application (Go monolith) at `app.restoredrill.io`.
The marketing site lives in a separate repo.

## Status

Phase 1 skeleton. Boots a Chi + Templ + HTMX + Tailwind server with a
Postgres-backed session, signup/login flow, and an empty dashboard.

## Local development

```sh
make dev
```

This starts Postgres in Docker, runs migrations, builds CSS, regenerates
Templ files, and runs the server on `http://localhost:8080`.

## Layout

```
cmd/server          HTTP + worker entrypoint
cmd/migrate         goose CLI wrapper
internal/auth       sessions, password hashing, RBAC
internal/db         pgx pool, transaction helpers
internal/web        handlers + Templ templates
migrations          goose SQL migrations
assets              Tailwind input, static files
```

See [`docs/plan.md`](docs/plan.md) for the full plan against the
11-layer rubric, including the active phase brief.
