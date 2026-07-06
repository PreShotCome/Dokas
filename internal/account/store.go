// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

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
	RoleOwner   Role = "owner"
	RoleAdmin   Role = "admin"
	RoleMember  Role = "member"
	RoleViewer  Role = "viewer"
	RoleExec    Role = "exec"    // internal executive dashboard access (read-only)
	RoleAuditor Role = "auditor" // external auditor: drills/evidence only, no billing or roster
)

// Valid reports whether r is one of the known roles. Used at the form
// boundary so we don't trust client-submitted role strings.
func (r Role) Valid() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleMember, RoleViewer, RoleExec, RoleAuditor:
		return true
	}
	return false
}

// ValidInviteRole rejects RoleOwner — there's exactly one owner per account
// (the creator); ownership transfers happen through TransferOwnership only.
func (r Role) ValidInviteRole() bool {
	return r == RoleAdmin || r == RoleMember || r == RoleViewer ||
		r == RoleExec || r == RoleAuditor
}

type Plan string

const (
	PlanTrial   Plan = "trial"
	PlanStarter Plan = "starter"
	PlanPro     Plan = "pro"   // "Growth" tier on the marketing page
	PlanScale   Plan = "scale" // high-volume self-serve tier above Growth
)

// IsPaid reports whether a plan is a paid subscription (Starter or Pro/Growth)
// rather than the free trial. Free/trial accounts may only drill the built-in
// sample dataset; drilling a real backup requires a paid plan.
func IsPaid(p Plan) bool { return p == PlanStarter || p == PlanPro || p == PlanScale }

type Account struct {
	ID               uuid.UUID
	Name             string
	Slug             string
	StripeCustomerID *string
	Plan             Plan
	CreatedAt        time.Time
	// TrialEndsAt is when a trial-plan account's full-access window closes.
	// Nil on paid plans (and on trial accounts predating the trial window).
	TrialEndsAt *time.Time
	// SubscriptionStatus is the Stripe subscription state ("active", "trialing",
	// "past_due", "unpaid", "canceled", "incomplete", "paused") — nil when the
	// account has never had a subscription. Used to distinguish a paying
	// customer whose card just failed (past_due) from a never-paid trial, so
	// the payment-issue banner and the trial-ended banner don't overlap.
	SubscriptionStatus *string
	// Unlimited short-circuits every resource cap, cadence gate, trial
	// paywall, drill quota, dump-size check, and dunning/trial banner. The
	// founder/staff-owned account flips this so operating the product does
	// not fight its own guardrails. Default false; flipped via fly-admin
	// set-unlimited.
	Unlimited bool
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
	ErrNotFound       = errors.New("account: not found")
	ErrInvitationGone = errors.New("account: invitation expired or already accepted")
	// ErrInvitationWrongEmail is returned when the accepting user's email
	// does not match the address the invitation was issued to.
	ErrInvitationWrongEmail = errors.New("account: invitation was issued to a different email")
	ErrSlugUnavailable      = errors.New("account: slug unavailable")
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
		INSERT INTO accounts (name, slug, trial_ends_at)
		VALUES ($1, $2, now() + interval '14 days')
		RETURNING id, name, slug, stripe_customer_id, plan, created_at, trial_ends_at, subscription_status, is_unlimited
	`, name, slug).Scan(&acct.ID, &acct.Name, &acct.Slug, &acct.StripeCustomerID, &acct.Plan,
		&acct.CreatedAt, &acct.TrialEndsAt, &acct.SubscriptionStatus, &acct.Unlimited)
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
		SELECT id, name, slug, stripe_customer_id, plan, created_at, trial_ends_at, subscription_status, is_unlimited
		  FROM accounts WHERE id = $1 AND deleted_at IS NULL
	`, accountID).Scan(&a.ID, &a.Name, &a.Slug, &a.StripeCustomerID, &a.Plan, &a.CreatedAt, &a.TrialEndsAt, &a.SubscriptionStatus, &a.Unlimited)
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

// SetUnlimited flips the founder/staff "no caps, no paywalls" flag on an
// account. Not exposed through any customer-facing surface — set from ops
// (fly-admin set-unlimited) only.
func (s *Store) SetUnlimited(ctx context.Context, accountID uuid.UUID, unlimited bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE accounts SET is_unlimited = $2 WHERE id = $1`, accountID, unlimited)
	return err
}

// SetUnlimitedByOwnerEmail resolves the account that `email` owns (RoleOwner)
// and flips is_unlimited. Returns the affected account ID for logging. Errors
// if the email owns nothing or owns multiple accounts (safer to be explicit
// with the accountID in that case).
func (s *Store) SetUnlimitedByOwnerEmail(ctx context.Context, email string, unlimited bool) (uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id
		  FROM accounts a
		  JOIN memberships m ON m.account_id = a.id AND m.role = 'owner'
		  JOIN users u ON u.id = m.user_id
		 WHERE lower(u.email) = lower($1) AND a.deleted_at IS NULL
	`, email)
	if err != nil {
		return uuid.Nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return uuid.Nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return uuid.Nil, err
	}
	switch len(ids) {
	case 0:
		return uuid.Nil, errors.New("account: no account owned by that email")
	case 1:
		if err := s.SetUnlimited(ctx, ids[0], unlimited); err != nil {
			return uuid.Nil, err
		}
		return ids[0], nil
	default:
		return uuid.Nil, errors.New("account: email owns multiple accounts — set by ID")
	}
}

// SyncSubscription updates an account's plan and subscription state from a
// Stripe webhook, matched by Stripe customer ID. eventCreated is the Stripe
// event.created — older-than-current rows are skipped so out-of-order
// deliveries cannot resurrect a canceled account by overwriting newer state.
// An unknown customer is a no-op (the event is for an account this server
// doesn't hold).
func (s *Store) SyncSubscription(ctx context.Context, customerID, subscriptionID, status, plan string, eventCreated time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE accounts
		   SET plan = $2,
		       stripe_subscription_id = NULLIF($3, ''),
		       subscription_status    = NULLIF($4, ''),
		       subscription_status_updated_at = $5
		 WHERE stripe_customer_id = $1
		   AND (subscription_status_updated_at IS NULL
		        OR subscription_status_updated_at <= $5)
	`, customerID, plan, subscriptionID, status, eventCreated)
	return err
}

// HandleSubscriptionCanceled is the deleted-subscription path: drop the
// account back to the trial tier, clear the Stripe subscription ID, mark
// status canceled, AND extend trial_ends_at to a short grace window so the
// cancellation does not instantly lock the account out — the owner has time
// to re-subscribe through Checkout (which now allows it because
// stripe_subscription_id is NULL again).
func (s *Store) HandleSubscriptionCanceled(ctx context.Context, customerID string, eventCreated, graceUntil time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE accounts
		   SET plan                          = 'trial',
		       stripe_subscription_id        = NULL,
		       subscription_status           = 'canceled',
		       subscription_status_updated_at = $2,
		       trial_ends_at                 = $3
		 WHERE stripe_customer_id = $1
		   AND (subscription_status_updated_at IS NULL
		        OR subscription_status_updated_at <= $2)
	`, customerID, eventCreated, graceUntil)
	return err
}

// RecordStripeEvent inserts a Stripe event ID into the dedup table. Returns
// OwnerContact is an account's owner's email + display name — used by the
// billing webhook to send dunning / cancellation notices to the person who
// signed up. Returns pgx.ErrNoRows when no owner or no matching customer.
type OwnerContact struct {
	AccountID   uuid.UUID
	AccountName string
	OwnerEmail  string
}

// OwnerByStripeCustomer resolves a Stripe customer ID back to the account's
// owner. Used by the billing webhook to address dunning / cancellation email.
func (s *Store) OwnerByStripeCustomer(ctx context.Context, customerID string) (OwnerContact, error) {
	var o OwnerContact
	err := s.pool.QueryRow(ctx, `
		SELECT a.id, a.name, u.email
		  FROM accounts a
		  JOIN memberships m ON m.account_id = a.id AND m.role = 'owner'
		  JOIN users u ON u.id = m.user_id
		 WHERE a.stripe_customer_id = $1 AND a.deleted_at IS NULL
		 LIMIT 1
	`, customerID).Scan(&o.AccountID, &o.AccountName, &o.OwnerEmail)
	return o, err
}

// true on first-seen, false on duplicate. Stripe retries 5xx for ~3 days and
// may also replay older events; this is the gate that makes the webhook
// handler idempotent.
func (s *Store) RecordStripeEvent(ctx context.Context, eventID string) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO stripe_events (event_id) VALUES ($1) ON CONFLICT DO NOTHING`, eventID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
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

// lockMembers locks every membership row of an account FOR UPDATE and returns
// the owner count plus the target user's role. Holding the lock serialises
// concurrent role changes, so the last-owner check below cannot be raced into
// leaving an account with zero owners.
func lockMembers(ctx context.Context, tx pgx.Tx, accountID, userID uuid.UUID) (ownerCount int, targetRole Role, found bool, err error) {
	rows, err := tx.Query(ctx, `
		SELECT user_id, role FROM memberships WHERE account_id = $1 FOR UPDATE
	`, accountID)
	if err != nil {
		return 0, "", false, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid uuid.UUID
		var role Role
		if err := rows.Scan(&uid, &role); err != nil {
			return 0, "", false, err
		}
		if role == RoleOwner {
			ownerCount++
		}
		if uid == userID {
			targetRole, found = role, true
		}
	}
	return ownerCount, targetRole, found, rows.Err()
}

func (s *Store) UpdateMemberRole(ctx context.Context, accountID, userID uuid.UUID, role Role) error {
	if !role.Valid() {
		return errors.New("invalid role")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	ownerCount, currentRole, found, err := lockMembers(ctx, tx, accountID, userID)
	if err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	// Forbid demoting the last owner — would leave the account ownerless.
	if role != RoleOwner && currentRole == RoleOwner && ownerCount <= 1 {
		return errors.New("cannot demote the only owner")
	}
	if _, err := tx.Exec(ctx, `
		UPDATE memberships SET role = $3 WHERE account_id = $1 AND user_id = $2
	`, accountID, userID, role); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// TransferOwnership hands the owner role from the current owner to another
// member, demoting the old owner to admin — atomically, so the account
// always has exactly one owner. fromUserID must currently be the owner and
// toUserID must already be a member.
func (s *Store) TransferOwnership(ctx context.Context, accountID, fromUserID, toUserID uuid.UUID) error {
	if fromUserID == toUserID {
		return errors.New("account: cannot transfer ownership to the current owner")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var fromRole Role
	if err := tx.QueryRow(ctx, `
		SELECT role FROM memberships WHERE account_id = $1 AND user_id = $2
	`, accountID, fromUserID).Scan(&fromRole); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if fromRole != RoleOwner {
		return errors.New("account: only the current owner can transfer ownership")
	}

	var toExists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM memberships WHERE account_id = $1 AND user_id = $2)
	`, accountID, toUserID).Scan(&toExists); err != nil {
		return err
	}
	if !toExists {
		return ErrNotFound
	}

	if _, err := tx.Exec(ctx, `
		UPDATE memberships SET role = 'admin' WHERE account_id = $1 AND user_id = $2
	`, accountID, fromUserID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE memberships SET role = 'owner' WHERE account_id = $1 AND user_id = $2
	`, accountID, toUserID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) RemoveMember(ctx context.Context, accountID, userID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	ownerCount, role, found, err := lockMembers(ctx, tx, accountID, userID)
	if err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	if role == RoleOwner && ownerCount <= 1 {
		return errors.New("cannot remove the only owner")
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM memberships WHERE account_id = $1 AND user_id = $2
	`, accountID, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
// no-op. userEmail must match the address the invitation was sent to —
// otherwise anyone holding the link could join the account.
func (s *Store) AcceptInvitation(ctx context.Context, rawToken string, userID uuid.UUID, userEmail string) (Invitation, error) {
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
	if !strings.EqualFold(strings.TrimSpace(userEmail), inv.Email) {
		return inv, ErrInvitationWrongEmail
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
