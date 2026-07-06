// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package drill_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preshotcome/dokaz/internal/account"
	"github.com/preshotcome/dokaz/internal/drill"
)

// addMember inserts a second user into an existing account with the given role
// and returns the user ID. Used to exercise team scoping for a non-owner.
func addMember(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accountID uuid.UUID, role string) uuid.UUID {
	t.Helper()
	userID := uuid.New()
	email := "member+" + userID.String() + "@example.com"
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'x')`, userID, email); err != nil {
		t.Fatalf("insert member user: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID) })
	if _, err := pool.Exec(ctx, `INSERT INTO memberships (account_id, user_id, role) VALUES ($1, $2, $3)`, accountID, userID, role); err != nil {
		t.Fatalf("insert membership: %v", err)
	}
	return userID
}

func mkTarget(t *testing.T, ctx context.Context, s *drill.Store, accountID, userID uuid.UUID, name string) drill.Target {
	t.Helper()
	tg, err := s.CreateTarget(ctx, drill.Target{
		AccountID: accountID, CreatedByUserID: userID, Name: name,
		SourceKind: "postgres_dump_local", SourceURI: "/tmp/" + name + ".dump",
	})
	if err != nil {
		t.Fatalf("create target %s: %v", name, err)
	}
	return tg
}

// TestTeamScoping is the security core of issue #29: a non-privileged member
// sees only their teams' databases plus unassigned ones, never another team's,
// while ScopeAll (owner/admin) sees everything.
func TestTeamScoping(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	accounts := account.NewStore(pool)
	store := drill.NewStore(pool)
	ownerID, accountID := seedUserAccount(t, ctx, pool, accounts)
	memberID := addMember(t, ctx, pool, accountID, "member")

	// Two teams, three databases: t1→Payments, t2→Analytics, t3 unassigned.
	payments, err := accounts.CreateTeam(ctx, accountID, "Payments")
	if err != nil {
		t.Fatalf("create team payments: %v", err)
	}
	analytics, err := accounts.CreateTeam(ctx, accountID, "Analytics")
	if err != nil {
		t.Fatalf("create team analytics: %v", err)
	}
	t1 := mkTarget(t, ctx, store, accountID, ownerID, "pay-db")
	t2 := mkTarget(t, ctx, store, accountID, ownerID, "analytics-db")
	t3 := mkTarget(t, ctx, store, accountID, ownerID, "shared-db")
	mustSetTeam(t, ctx, store, accountID, t1.ID, &payments.ID)
	mustSetTeam(t, ctx, store, accountID, t2.ID, &analytics.ID)

	// The member is on Payments only.
	if err := accounts.AddTeamMember(ctx, accountID, payments.ID, memberID); err != nil {
		t.Fatalf("add team member: %v", err)
	}
	teamIDs, err := accounts.TeamIDsForUser(ctx, accountID, memberID)
	if err != nil || len(teamIDs) != 1 || teamIDs[0] != payments.ID {
		t.Fatalf("TeamIDsForUser = %v (err %v), want [payments]", teamIDs, err)
	}
	memberScope := drill.Scope{TeamIDs: teamIDs}

	// Member sees Payments (t1) + unassigned (t3), NOT Analytics (t2).
	got := targetNames(t, ctx, store, accountID, memberScope)
	if !got["pay-db"] || !got["shared-db"] || got["analytics-db"] {
		t.Fatalf("member scope saw %v, want pay-db+shared-db but not analytics-db", got)
	}
	if len(got) != 2 {
		t.Fatalf("member scope saw %d databases, want 2", len(got))
	}

	// Owner/admin (ScopeAll) sees all three.
	all := targetNames(t, ctx, store, accountID, drill.ScopeAll())
	if len(all) != 3 {
		t.Fatalf("ScopeAll saw %d databases, want 3", len(all))
	}

	// Direct get of another team's database 404s for the member but not for all.
	if _, err := store.GetTarget(ctx, accountID, t2.ID, memberScope); err != drill.ErrNotFound {
		t.Fatalf("member GetTarget(analytics db): got %v, want ErrNotFound", err)
	}
	if _, err := store.GetTarget(ctx, accountID, t2.ID, drill.ScopeAll()); err != nil {
		t.Fatalf("ScopeAll GetTarget(analytics db): %v", err)
	}
	if _, err := store.GetTarget(ctx, accountID, t1.ID, memberScope); err != nil {
		t.Fatalf("member GetTarget(their own team db): %v", err)
	}

	// A drill on the analytics database is invisible to the member.
	drillID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO drills (id, target_id, account_id, created_by_user_id, status)
		VALUES ($1, $2, $3, $4, 'succeeded')`, drillID, t2.ID, accountID, ownerID); err != nil {
		t.Fatalf("insert drill: %v", err)
	}
	if _, err := store.GetDrill(ctx, accountID, drillID, memberScope); err != drill.ErrNotFound {
		t.Fatalf("member GetDrill(analytics drill): got %v, want ErrNotFound", err)
	}
	if _, err := store.GetDrill(ctx, accountID, drillID, drill.ScopeAll()); err != nil {
		t.Fatalf("ScopeAll GetDrill(analytics drill): %v", err)
	}

	// Cross-account assignment is rejected: a team from another account can't
	// claim this account's database.
	_, otherAccount := seedUserAccount(t, ctx, pool, accounts)
	otherTeam, err := accounts.CreateTeam(ctx, otherAccount, "Outside")
	if err != nil {
		t.Fatalf("create other team: %v", err)
	}
	if err := store.SetTargetTeam(ctx, accountID, t3.ID, &otherTeam.ID); err != drill.ErrNotFound {
		t.Fatalf("cross-account assign: got %v, want ErrNotFound", err)
	}
	// t3 stayed unassigned — still visible to the member.
	if got := targetNames(t, ctx, store, accountID, memberScope); !got["shared-db"] {
		t.Fatal("shared-db should remain visible after a rejected cross-account assign")
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM drills WHERE account_id = $1`, accountID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM database_targets WHERE account_id = $1`, accountID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM teams WHERE account_id IN ($1, $2)`, accountID, otherAccount)
	})
}

func mustSetTeam(t *testing.T, ctx context.Context, s *drill.Store, accountID, targetID uuid.UUID, teamID *uuid.UUID) {
	t.Helper()
	if err := s.SetTargetTeam(ctx, accountID, targetID, teamID); err != nil {
		t.Fatalf("set target team: %v", err)
	}
}

func targetNames(t *testing.T, ctx context.Context, s *drill.Store, accountID uuid.UUID, scope drill.Scope) map[string]bool {
	t.Helper()
	ts, err := s.ListTargets(ctx, accountID, scope)
	if err != nil {
		t.Fatalf("list targets: %v", err)
	}
	out := make(map[string]bool, len(ts))
	for _, tg := range ts {
		out[tg.Name] = true
	}
	return out
}
