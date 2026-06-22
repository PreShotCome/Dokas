// Package dbmigrate applies the application's schema migrations (goose) and
// then River's queue migrations against a database. It reads migrations
// embedded in the binary (see package migrations), so it runs identically
// from the migrate CLI and from the server's startup path — a deployed binary
// carries everything it needs and no flaky external release step is required.
package dbmigrate

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/preshotcome/dokaz/migrations"
)

// Up applies all pending goose migrations and then River's queue migrations.
// It is safe to run on every server start: goose and River each track applied
// versions, so an up-to-date database is a no-op.
func Up(ctx context.Context, url string) error {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("dialect: %w", err)
	}
	goose.SetBaseFS(migrations.FS)
	if err := goose.UpContext(ctx, db, "."); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return riverUp(ctx, url)
}

// riverUp runs River's own migrations (it keeps a separate river_migration
// table in the same database).
func riverUp(ctx context.Context, url string) error {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return fmt.Errorf("pool: %w", err)
	}
	defer pool.Close()
	mig, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("river migrator: %w", err)
	}
	if _, err := mig.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("river migrate: %w", err)
	}
	return nil
}
