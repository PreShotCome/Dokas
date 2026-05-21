// Package account owns the multi-tenant data model: accounts, memberships,
// and invitations. Drills and targets are scoped to accounts; users join
// accounts through memberships and carry a role on each one.
package account

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Role enumerates the four RBAC roles. The string form matches the DB
// constraint so direct assignment is safe.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

// Valid reports whether r is one of the four known roles. Used at the form
// boundary so we don't trust client-submitted role strings.
func (r Role) Valid() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleMember, RoleViewer:
		return true
	}
	return false
}

// ValidInviteRole rejects RoleOwner — there's exactly one owner per account
// (the creator), and ownership transfers are out of scope for Phase 3.
func (r Role) ValidInviteRole() bool {
	return r == RoleAdmin || r == RoleMember || r == RoleViewer
}

type Plan string

const (
	PlanTrial   Plan = "trial"
	PlanStarter Plan = "starter"
	PlanPro     Plan = "pro"
)

type Account struct {
	ID               uuid.UUID
	Name             string
	Slug             string
	StripeCustomerID *string
	Plan             Plan
	CreatedAt        time.Time
}

type Membership struct {
	AccountID uuid.UUID
	UserID    uuid.UUID
	Role      Role
	CreatedAt time.Time
}

// MembershipWithUser is the join used by the members page.
type MembershipWithUser struct {
	Membership
	Email string
}

type Invitation struct {
	ID              uuid.UUID
	AccountID       uuid.UUID
	Email           string
	Role            Role
	InvitedByUserID uuid.UUID
	ExpiresAt       time.Time
	AcceptedAt      *time.Time
	CreatedAt       time.Time
}

var (
	ErrNotFound        = errors.New("account: not found")
	ErrInvitationGone  = errors.New("account: invitation expired or already accepted")
	ErrSlugUnavailable = errors.New("account: slug unavailable")
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// CreatePersonalAccount is the helper called from signup. It creates an
// account named "<local>'s workspace" with a slug derived from the email
// local-part + a short user-id suffix (matches the migration backfill
// scheme), and inserts the owner membership in the same transaction.
func (s *Store) CreatePersonalAccount(ctx context.Context, userID uuid.UUID, email string) (Account, error) {
	local := emailLocal(email)
	name := local + "'s workspace"
	slug := slugify(local) + "-" + shortID(userID)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var acct Account
	err = tx.QueryRow(ctx, `
		INSERT INTO accounts (name, slug)
		VALUES ($1, $2)
		RETURNING id, name, slug, stripe_customer_id, plan, created_at
	`, name, slug).Scan(&acct.ID, &acct.Name, &acct.Slug, &acct.StripeCustomerID, &acct.Plan, &acct.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return Account{}, ErrSlugUnavailable
		}
		return Account{}, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO memberships (account_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, acct.ID, userID); err != nil {
		return Account{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Account{}, err
	}
	return acct, nil
}

// GetAccount loads by ID. Does not check membership — callers must.
func (s *Store) GetAccount(ctx context.Context, accountID uuid.UUID) (Account, error) {
	var a Account
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, slug, stripe_customer_id, plan, created_at
		  FROM accounts WHERE id = $1 AND deleted_at IS NULL
	`, accountID).Scan(&a.ID, &a.Name, &a.Slug, &a.StripeCustomerID, &a.Plan, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	return a, err
}

// SetStripeCustomerID is called from the billing layer after a Stripe
// Customer is created. Separate from Account creation so signup doesn't
// block on Stripe.
func (s *Store) SetStripeCustomerID(ctx context.Context, accountID uuid.UUID, customerID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE accounts SET stripe_customer_id = $2 WHERE id = $1`, accountID, customerID)
	return err
}

// ListAccountsForUser returns every account the user is a member of, with
// their role. Used by the account switcher in the nav.
func (s *Store) ListAccountsForUser(ctx context.Context, userID uuid.UUID) ([]struct {
	Account
	Role Role
}, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.name, a.slug, a.stripe_customer_id, a.plan, a.created_at, m.role
		  FROM memberships m
		  JOIN accounts a ON a.id = m.account_id
		 WHERE m.user_id = $1 AND a.deleted_at IS NULL
		 ORDER BY a.created_at ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		Account
		Role Role
	}
	for rows.Next() {
		var row struct {
			Account
			Role Role
		}
		if err := rows.Scan(&row.ID, &row.Name, &row.Slug, &row.StripeCustomerID, &row.Plan, &row.CreatedAt, &row.Role); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// GetMembership returns the user's role on the account, or ErrNotFound if
// they're not a member. The middleware uses this on every authenticated
// request to enforce tenancy.
func (s *Store) GetMembership(ctx context.Context, accountID, userID uuid.UUID) (Membership, error) {
	var m Membership
	err := s.pool.QueryRow(ctx, `
		SELECT account_id, user_id, role, created_at
		  FROM memberships WHERE account_id = $1 AND user_id = $2
	`, accountID, userID).Scan(&m.AccountID, &m.UserID, &m.Role, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Membership{}, ErrNotFound
	}
	return m, err
}

// ListMembers returns every member of an account with their email.
func (s *Store) ListMembers(ctx context.Context, accountID uuid.UUID) ([]MembershipWithUser, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.account_id, m.user_id, m.role, m.created_at, u.email::text
		  FROM memberships m
		  JOIN users u ON u.id = m.user_id
		 WHERE m.account_id = $1 AND u.deleted_at IS NULL
		 ORDER BY m.created_at ASC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MembershipWithUser
	for rows.Next() {
		var m MembershipWithUser
		if err := rows.Scan(&m.AccountID, &m.UserID, &m.Role, &m.CreatedAt, &m.Email); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) UpdateMemberRole(ctx context.Context, accountID, userID uuid.UUID, role Role) error {
	if !role.Valid() {
		return errors.New("invalid role")
	}
	// Forbid demoting the last owner — would leave the account ownerless.
	if role != RoleOwner {
		var ownerCount int
		if err := s.pool.QueryRow(ctx, `
			SELECT count(*) FROM memberships WHERE account_id = $1 AND role = 'owner'
		`, accountID).Scan(&ownerCount); err != nil {
			return err
		}
		var currentRole Role
		if err := s.pool.QueryRow(ctx, `
			SELECT role FROM memberships WHERE account_id = $1 AND user_id = $2
		`, accountID, userID).Scan(&currentRole); err != nil {
			return err
		}
		if currentRole == RoleOwner && ownerCount <= 1 {
			return errors.New("cannot demote the only owner")
		}
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE memberships SET role = $3 WHERE account_id = $1 AND user_id = $2
	`, accountID, userID, role)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RemoveMember(ctx context.Context, accountID, userID uuid.UUID) error {
	// Mirror the owner protection above.
	var ownerCount int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM memberships WHERE account_id = $1 AND role = 'owner'
	`, accountID).Scan(&ownerCount); err != nil {
		return err
	}
	var role Role
	if err := s.pool.QueryRow(ctx, `
		SELECT role FROM memberships WHERE account_id = $1 AND user_id = $2
	`, accountID, userID).Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if role == RoleOwner && ownerCount <= 1 {
		return errors.New("cannot remove the only owner")
	}
	_, err := s.pool.Exec(ctx, `
		DELETE FROM memberships WHERE account_id = $1 AND user_id = $2
	`, accountID, userID)
	return err
}

// --- invitations ---

// CreateInvitation issues a fresh invitation. Returns the raw token (sent in
// the link) and the stored row. We only persist the SHA-256 hash of the
// token, mirroring the session model.
func (s *Store) CreateInvitation(ctx context.Context, accountID, invitedBy uuid.UUID, email string, role Role, ttl time.Duration) (string, Invitation, error) {
	if !role.ValidInviteRole() {
		return "", Invitation{}, errors.New("invalid invite role")
	}
	raw, hash, err := generateInviteToken()
	if err != nil {
		return "", Invitation{}, err
	}
	inv := Invitation{
		AccountID:       accountID,
		Email:           strings.TrimSpace(strings.ToLower(email)),
		Role:            role,
		InvitedByUserID: invitedBy,
		ExpiresAt:       time.Now().UTC().Add(ttl),
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO invitations (account_id, email, role, token_hash, invited_by_user_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at
	`, inv.AccountID, inv.Email, inv.Role, hash, inv.InvitedByUserID, inv.ExpiresAt).Scan(&inv.ID, &inv.CreatedAt)
	if err != nil {
		return "", Invitation{}, err
	}
	return raw, inv, nil
}

// LookupInvitation resolves the raw token back to a pending row, returning
// ErrInvitationGone for expired or already-accepted tokens.
func (s *Store) LookupInvitation(ctx context.Context, rawToken string) (Invitation, error) {
	hash := hashToken(rawToken)
	var inv Invitation
	err := s.pool.QueryRow(ctx, `
		SELECT id, account_id, email::text, role, invited_by_user_id, expires_at, accepted_at, created_at
		  FROM invitations WHERE token_hash = $1
	`, hash).Scan(&inv.ID, &inv.AccountID, &inv.Email, &inv.Role, &inv.InvitedByUserID,
		&inv.ExpiresAt, &inv.AcceptedAt, &inv.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, ErrNotFound
	}
	if err != nil {
		return Invitation{}, err
	}
	if inv.AcceptedAt != nil || time.Now().UTC().After(inv.ExpiresAt) {
		return inv, ErrInvitationGone
	}
	return inv, nil
}

// AcceptInvitation marks the invitation accepted and creates the membership
// in one transaction. Idempotent: a second accept by the same user is a
// no-op.
func (s *Store) AcceptInvitation(ctx context.Context, rawToken string, userID uuid.UUID) (Invitation, error) {
	hash := hashToken(rawToken)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Invitation{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var inv Invitation
	err = tx.QueryRow(ctx, `
		SELECT id, account_id, email::text, role, invited_by_user_id, expires_at, accepted_at, created_at
		  FROM invitations WHERE token_hash = $1 FOR UPDATE
	`, hash).Scan(&inv.ID, &inv.AccountID, &inv.Email, &inv.Role, &inv.InvitedByUserID,
		&inv.ExpiresAt, &inv.AcceptedAt, &inv.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, ErrNotFound
	}
	if err != nil {
		return Invitation{}, err
	}
	if inv.AcceptedAt != nil {
		return inv, ErrInvitationGone
	}
	if time.Now().UTC().After(inv.ExpiresAt) {
		return inv, ErrInvitationGone
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO memberships (account_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (account_id, user_id) DO NOTHING
	`, inv.AccountID, userID, inv.Role); err != nil {
		return Invitation{}, err
	}

	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
		UPDATE invitations SET accepted_at = $2 WHERE id = $1
	`, inv.ID, now); err != nil {
		return Invitation{}, err
	}
	inv.AcceptedAt = &now

	if err := tx.Commit(ctx); err != nil {
		return Invitation{}, err
	}
	return inv, nil
}

// ListPendingInvitations returns un-accepted invitations for an account.
func (s *Store) ListPendingInvitations(ctx context.Context, accountID uuid.UUID) ([]Invitation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, account_id, email::text, role, invited_by_user_id, expires_at, accepted_at, created_at
		  FROM invitations WHERE account_id = $1 AND accepted_at IS NULL
		  ORDER BY created_at DESC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invitation
	for rows.Next() {
		var inv Invitation
		if err := rows.Scan(&inv.ID, &inv.AccountID, &inv.Email, &inv.Role, &inv.InvitedByUserID,
			&inv.ExpiresAt, &inv.AcceptedAt, &inv.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// --- helpers ---

func emailLocal(email string) string {
	if i := strings.Index(email, "@"); i > 0 {
		return email[:i]
	}
	return email
}

var slugStripRe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(s)
	s = slugStripRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "user"
	}
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

func shortID(id uuid.UUID) string {
	hex := strings.ReplaceAll(id.String(), "-", "")
	return hex[:6]
}

func isUniqueViolation(err error) bool {
	type sqlState interface{ SQLState() string }
	var s sqlState
	if errors.As(err, &s) {
		return s.SQLState() == "23505"
	}
	return strings.Contains(strings.ToLower(err.Error()), "duplicate")
}

// Invitation tokens use the same generate/hash pattern as session tokens.
// We keep the implementation here (not import auth) to avoid coupling the
// account package to the cookie session model.
func generateInviteToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return base64.RawStdEncoding.EncodeToString(sum[:])
}

// ParseRole is the safe boundary parser used by handlers.
func ParseRole(s string) (Role, error) {
	r := Role(strings.ToLower(strings.TrimSpace(s)))
	if !r.Valid() {
		return "", fmt.Errorf("invalid role: %q", s)
	}
	return r, nil
}
