package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr,
			"usage: migrate <command> [args]\n"+
				"commands: up, up-by-one, down, status, reset, version, create <name> sql\n"+
				"env: DATABASE_URL (required), MIGRATIONS_DIR (defaults to ./migrations)\n",
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
}

func fail(stage string, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		return
	}
	fmt.Fprintf(os.Stderr, "%s: %v\n", stage, err)
	os.Exit(1)
}
