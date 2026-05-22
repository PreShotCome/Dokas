# CLAUDE.md

Guidance for Claude Code sessions working in this repository.

## Project

Soteria — a B2B backup-verification SaaS (Go monolith: Chi + Templ +
HTMX + Tailwind + Postgres + River). The full plan and the deferred-items
log live in `docs/plan.md` and `docs/backlog.md` — read them first.

## Branch & merge workflow

- Develop on `claude/soteria-phase-2-vHjzy`. Commit work there.
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
