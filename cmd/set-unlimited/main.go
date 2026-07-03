// Command set-unlimited flips the is_unlimited flag on an account, resolved
// by the account owner's email. Intended for ops use only — invoked via
// `flyctl ssh console -a dokaz -C '/app/set-unlimited --email <owner> [--off]'`
// from the fly-admin workflow. Prints the affected account ID on success.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preshotcome/dokaz/internal/account"
)

func main() {
	email := flag.String("email", "", "owner email of the account to flip")
	off := flag.Bool("off", false, "clear the flag instead of setting it")
	flag.Parse()
	if *email == "" {
		fmt.Fprintln(os.Stderr, "set-unlimited: --email is required")
		os.Exit(2)
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "set-unlimited: DATABASE_URL is unset")
		os.Exit(2)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "set-unlimited: connect:", err)
		os.Exit(1)
	}
	defer pool.Close()

	store := account.NewStore(pool)
	unlimited := !*off
	id, err := store.SetUnlimitedByOwnerEmail(ctx, *email, unlimited)
	if err != nil {
		fmt.Fprintln(os.Stderr, "set-unlimited:", err)
		os.Exit(1)
	}
	verb := "SET"
	if *off {
		verb = "CLEARED"
	}
	fmt.Printf("%s is_unlimited on account %s (owner: %s)\n", verb, id, *email)
}
