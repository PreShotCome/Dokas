// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

// Command evidence-keys is the ops tool for the evidence master-key lifecycle.
// It reads DATABASE_URL, EVIDENCE_ENCRYPTION_KEY, and (optionally)
// EVIDENCE_ENCRYPTION_KEYS_RETIRED from the environment, so it is run on the
// app host where those already live:
//
//	flyctl ssh console -a dokaz -C '/app/evidence-keys verify'
//	flyctl ssh console -a dokaz -C '/app/evidence-keys rewrap'
//	flyctl ssh console -a dokaz -C '/app/evidence-keys shred-poison --yes'
//
// Subcommands:
//
//	verify        classify every account DEK: active / retired / poison
//	rewrap        re-wrap retired-key DEKs under the active key (finish a rotation)
//	shred-poison  delete DEK rows no key can unwrap (dry-run without --yes)
//
// See docs/runbooks/evidence-key-rotation.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preshotcome/dokaz/internal/evidence"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	yes := fs.Bool("yes", false, "confirm the destructive shred-poison operation")
	_ = fs.Parse(os.Args[2:])

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		die("DATABASE_URL is unset")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		die("connect: " + err.Error())
	}
	defer pool.Close()

	c, err := evidence.NewCipher(
		os.Getenv("EVIDENCE_ENCRYPTION_KEY"),
		splitCSV(os.Getenv("EVIDENCE_ENCRYPTION_KEYS_RETIRED")),
		pool,
	)
	if err != nil {
		die(err.Error())
	}
	// Refuse to classify against a random ephemeral key — every row would look
	// poison and shred-poison would wipe the table.
	if c.Ephemeral() {
		die("EVIDENCE_ENCRYPTION_KEY is unset — refusing to run against an ephemeral key")
	}

	switch cmd {
	case "verify":
		a, err := c.Audit(ctx)
		if err != nil {
			die(err.Error())
		}
		fmt.Printf("active=%d retired=%d poison=%d\n", a.Active, a.Retired, len(a.Poison))
		for _, id := range a.Poison {
			fmt.Printf("  poison: %s\n", id)
		}
	case "rewrap":
		n, poison, err := c.RewrapAll(ctx)
		if err != nil {
			die(err.Error())
		}
		fmt.Printf("rewrapped=%d poison=%d\n", n, poison)
	case "shred-poison":
		a, err := c.Audit(ctx)
		if err != nil {
			die(err.Error())
		}
		if len(a.Poison) == 0 {
			fmt.Println("no poison rows — nothing to shred")
			return
		}
		if !*yes {
			fmt.Printf("DRY RUN — %d poison row(s) would be deleted (re-run with --yes):\n", len(a.Poison))
			for _, id := range a.Poison {
				fmt.Printf("  %s\n", id)
			}
			return
		}
		n, err := c.ShredPoison(ctx)
		if err != nil {
			die(err.Error())
		}
		fmt.Printf("deleted %d poison row(s)\n", n)
	default:
		usage()
	}
}

func splitCSV(v string) []string {
	if v == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, "evidence-keys:", msg)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: evidence-keys <verify|rewrap|shred-poison [--yes]>")
	os.Exit(2)
}
