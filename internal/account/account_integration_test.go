package account_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preshotcome/dokaz/internal/account"
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
	if _, err := store.AcceptInvitation(ctx, raw, inviteeID, "newbie@example.com"); err != nil {
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
	if _, err := store.AcceptInvitation(ctx, raw, inviteeID, "newbie@example.com"); err != account.ErrInvitationGone {
		t.Fatalf("re-accept = %v, want ErrInvitationGone", err)
	}

	// An invitation cannot be accepted by a mismatched email address.
	rawWrong, _, err := store.CreateInvitation(ctx, acct.ID, ownerID, "intended@example.com", account.RoleViewer, time.Hour)
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	if _, err := store.AcceptInvitation(ctx, rawWrong, inviteeID, "attacker@example.com"); err != account.ErrInvitationWrongEmail {
		t.Fatalf("wrong-email accept = %v, want ErrInvitationWrongEmail", err)
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

func TestTransferOwnership(t *testing.T) {
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
	memberID, _ := seedUser(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO memberships (account_id, user_id, role) VALUES ($1, $2, 'member')
	`, acct.ID, memberID); err != nil {
		t.Fatalf("add member: %v", err)
	}

	// Transferring to a non-member fails.
	stranger, _ := seedUser(t, ctx, pool)
	if err := store.TransferOwnership(ctx, acct.ID, ownerID, stranger); err == nil {
		t.Fatal("transfer to a non-member should fail")
	}

	// Transfer ownership to the member.
	if err := store.TransferOwnership(ctx, acct.ID, ownerID, memberID); err != nil {
		t.Fatalf("transfer ownership: %v", err)
	}
	newOwner, err := store.GetMembership(ctx, acct.ID, memberID)
	if err != nil || newOwner.Role != account.RoleOwner {
		t.Fatalf("new owner role = %s err=%v, want owner", newOwner.Role, err)
	}
	exOwner, err := store.GetMembership(ctx, acct.ID, ownerID)
	if err != nil || exOwner.Role != account.RoleAdmin {
		t.Fatalf("ex-owner role = %s err=%v, want admin", exOwner.Role, err)
	}

	// The ex-owner (now admin) can no longer transfer ownership.
	if err := store.TransferOwnership(ctx, acct.ID, ownerID, memberID); err == nil {
		t.Fatal("a non-owner should not be able to transfer ownership")
	}

	// Exactly one owner remains — the protection still holds.
	if err := store.RemoveMember(ctx, acct.ID, memberID); err == nil {
		t.Fatal("removing the sole (new) owner should fail")
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
