# CLAUDE.md

Guidance for Claude Code sessions working in this repository.

## Project

Dokaz — a B2B backup-verification SaaS (Go monolith: Chi + Templ +
HTMX + Tailwind + Postgres + River). The full plan and the deferred-items
log live in `docs/plan.md` and `docs/backlog.md` — read them first.

## Branch & merge workflow

- Develop on `claude/selket-phase-2-vHjzy`. Commit work there.
- **As the final step of every task, merge the dev branch into `main` and
  push `main`** — the user should not have to merge by hand.
- **Before pushing `main`, run the full CI pipeline locally and only push
  when it is green:**

  ```
  templ generate
  go mod tidy && git diff --exit-code go.mod go.sum   # tidy check
  go vet ./...
  govulncheck ./...                                    # must be clean
  npx tailwindcss -i assets/css/input.css -o assets/static/app.css --minify
  go run ./cmd/migrate up
  go test ./...
  go build ./cmd/server ./cmd/migrate
  ```

  If any step fails, fix it before `main` gets the push — never after.
- Do not open pull requests unless explicitly asked.

## Conventions

- `*_templ.go` files are generated — run `templ generate` after editing
  `.templ` files; commit the generated output.
- Every goose migration needs paired `+goose Up` / `+goose Down` sections
  (CI enforces this).
- Tests that need Postgres skip when `DATABASE_URL` is unset.

## End-to-end testing (no manual steps)

`scripts/e2e.sh` is the one-command, zero-input check. In a web session the
SessionStart hook has already started Postgres and exported `DATABASE_URL`,
so just run:

```
bash scripts/e2e.sh
```

It migrates, runs `go test ./...`, boots a real server, then runs two
crawlers against it and tears the server down:

- `cmd/linkcheck` — signs up a throwaway account and crawls every page
  (public + authenticated nav), failing on any broken link or error page.
- `cmd/e2e-smoke` — walks signup → sample drill → signed PDF and asserts
  the verdict is PASSED.

To link-check the **live** site without signing up (read-only, public
pages only):

```
BASE_URL=https://dokaz.net LINKCHECK_PUBLIC_ONLY=1 go run ./cmd/linkcheck
```
