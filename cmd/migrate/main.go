// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr,
			"usage: migrate <command> [args]\n"+
				"commands: up, up-by-one, down, status, reset, version, create <name> sql\n"+
				"env: DATABASE_URL (required), MIGRATIONS_DIR (defaults to ./migrations)\n\n"+
				"`up` applies app migrations (goose) and then River's queue migrations.\n",
		)
	}
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(2)
	}

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(2)
	}
	dir := os.Getenv("MIGRATIONS_DIR")
	if dir == "" {
		dir = "migrations"
	}

	db, err := sql.Open("pgx", url)
	if err != nil {
		fail("open db", err)
	}
	defer db.Close()

	if err := db.PingContext(context.Background()); err != nil {
		fail("ping db", err)
	}

	if err := goose.SetDialect("postgres"); err != nil {
		fail("dialect", err)
	}

	cmd := args[0]
	cmdArgs := args[1:]
	if err := goose.Run(cmd, db, dir, cmdArgs...); err != nil {
		fail("goose "+cmd, err)
	}

	// Run River's migrator after the app's goose migrations. Same DB; River
	// keeps its own schema in river_migration table.
	switch cmd {
	case "up", "up-by-one", "reset":
		if err := runRiverMigrate(url, cmd); err != nil {
			fail("river migrate", err)
		}
	}
}

func runRiverMigrate(url, gooseCmd string) error {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return fmt.Errorf("pool: %w", err)
	}
	defer pool.Close()

	mig, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("migrator: %w", err)
	}

	dir := rivermigrate.DirectionUp
	// `reset` in goose means "down all, up all". Honor that for River too —
	// migrate down first, then up.
	if gooseCmd == "reset" {
		if _, err := mig.Migrate(ctx, rivermigrate.DirectionDown, &rivermigrate.MigrateOpts{TargetVersion: -1}); err != nil {
			return fmt.Errorf("river reset: %w", err)
		}
	}
	_, err = mig.Migrate(ctx, dir, nil)
	return err
}

func fail(stage string, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		return
	}
	fmt.Fprintf(os.Stderr, "%s: %v\n", stage, err)
	os.Exit(1)
}
