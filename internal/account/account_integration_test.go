package account_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preshotcome/anything/internal/account"
)

// seedUser inserts a bare user row and returns its ID.
func seedUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (uuid.UUID, string) {
	t.Helper()
	id := uuid.New()
	email := "acct-test+" + id.String() + "@example.com"
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'x')
	`, id, email); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
	return id, email
}

func TestPersonalAccountAndMembership(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	store := account.NewStore(pool)

	userID, email := seedUser(t, ctx, pool)
	acct, err := store.CreatePersonalAccount(ctx, userID, email)
	if err != nil {
		t.Fatalf("create personal account: %v", err)
	}
	if acct.Plan != account.PlanTrial {
		t.Fatalf("new account plan = %s, want trial", acct.Plan)
	}

	m, err := store.GetMembership(ctx, acct.ID, userID)
	if err != nil {
		t.Fatalf("get membership: %v", err)
	}
	if m.Role != account.RoleOwner {
		t.Fatalf("creator role = %s, want owner", m.Role)
	}

	accounts, err := store.ListAccountsForUser(ctx, userID)
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if len(accounts) != 1 || accounts[0].ID != acct.ID {
		t.Fatalf("ListAccountsForUser = %+v, want [%s]", accounts, acct.ID)
	}
}

func TestInvitationLifecycle(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	store := account.NewStore(pool)

	ownerID, ownerEmail := seedUser(t, ctx, pool)
	acct, err := store.CreatePersonalAccount(ctx, ownerID, ownerEmail)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	inviteeID, _ := seedUser(t, ctx, pool)

	raw, inv, err := store.CreateInvitation(ctx, acct.ID, ownerID, "newbie@example.com", account.RoleMember, time.Hour)
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}

	// Lookup with the raw token resolves the pending row.
	found, err := store.LookupInvitation(ctx, raw)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if found.ID != inv.ID {
		t.Fatalf("lookup returned wrong invitation")
	}

	// Accept creates the membership.
	if _, err := store.AcceptInvitation(ctx, raw, inviteeID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	m, err := store.GetMembership(ctx, acct.ID, inviteeID)
	if err != nil {
		t.Fatalf("membership after accept: %v", err)
	}
	if m.Role != account.RoleMember {
		t.Fatalf("accepted role = %s, want member", m.Role)
	}

	// A second accept of the same token is rejected as gone.
	if _, err := store.AcceptInvitation(ctx, raw, inviteeID); err != account.ErrInvitationGone {
		t.Fatalf("re-accept = %v, want ErrInvitationGone", err)
	}

	// Expired invitations are rejected.
	rawExp, _, err := store.CreateInvitation(ctx, acct.ID, ownerID, "late@example.com", account.RoleViewer, -time.Minute)
	if err != nil {
		t.Fatalf("create expired invitation: %v", err)
	}
	if _, err := store.LookupInvitation(ctx, rawExp); err != account.ErrInvitationGone {
		t.Fatalf("expired lookup = %v, want ErrInvitationGone", err)
	}
}

func TestOwnerProtection(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	store := account.NewStore(pool)

	ownerID, ownerEmail := seedUser(t, ctx, pool)
	acct, err := store.CreatePersonalAccount(ctx, ownerID, ownerEmail)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	// The sole owner cannot be demoted or removed.
	if err := store.UpdateMemberRole(ctx, acct.ID, ownerID, account.RoleAdmin); err == nil {
		t.Fatalf("demoting the only owner should fail")
	}
	if err := store.RemoveMember(ctx, acct.ID, ownerID); err == nil {
		t.Fatalf("removing the only owner should fail")
	}
}
