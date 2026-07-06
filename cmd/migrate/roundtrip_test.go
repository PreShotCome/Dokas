// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package main

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// TestMigrationsRoundTrip applies every migration up, rolls every one back
// down to zero, then applies them up again — all against an isolated
// scratch database.
//
// CI's "Check migrations are reversible" step only greps for a +goose Down
// section; this test proves those down sections actually *run*, and that
// the schema survives a full up → down → up cycle (a down that drops
// objects in the wrong order, or names a renamed object, fails here).
func TestMigrationsRoundTrip(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()

	// A dedicated scratch database: the round trip drops every table, so it
	// must not run against the app database other tests share.
	const scratch = "migrate_roundtrip_test"
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	// FORCE disconnects any lingering session so a stale leftover drops cleanly.
	if _, err := admin.Exec(ctx, `DROP DATABASE IF EXISTS `+scratch+` WITH (FORCE)`); err != nil {
		admin.Close(ctx)
		t.Fatalf("drop stale scratch db: %v", err)
	}
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+scratch); err != nil {
		admin.Close(ctx)
		t.Fatalf("create scratch db: %v", err)
	}
	admin.Close(ctx)
	t.Cleanup(func() {
		a, err := pgx.Connect(ctx, adminURL)
		if err != nil {
			return
		}
		defer a.Close(ctx)
		_, _ = a.Exec(ctx, `DROP DATABASE IF EXISTS `+scratch+` WITH (FORCE)`)
	})

	scratchURL, err := swapDatabase(adminURL, scratch)
	if err != nil {
		t.Fatalf("build scratch DSN: %v", err)
	}
	db, err := sql.Open("pgx", scratchURL)
	if err != nil {
		t.Fatalf("open scratch db: %v", err)
	}
	defer db.Close()

	goose.SetVerbose(false)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("dialect: %v", err)
	}
	const dir = "../../migrations"

	if err := goose.Up(db, dir); err != nil {
		t.Fatalf("initial up: %v", err)
	}
	if err := goose.DownTo(db, dir, 0); err != nil {
		t.Fatalf("down to zero: %v", err)
	}
	if err := goose.Up(db, dir); err != nil {
		t.Fatalf("re-apply up after full rollback: %v", err)
	}
}

// swapDatabase returns a copy of dsn with its database name replaced,
// preserving auth, host, and query parameters.
func swapDatabase(dsn, dbName string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	u.Path = "/" + dbName
	return u.String(), nil
}
