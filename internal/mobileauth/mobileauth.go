// Package mobileauth issues and verifies bearer tokens for the native mobile
// app. Like internal/apikey, only a SHA-256 hash is stored, so a database leak
// doesn't expose usable tokens. Unlike API keys, a token is bound to a single
// user AND account: the app authenticates a person (via /mobile/login), not a
// machine, and is scoped to one workspace.
package mobileauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// tokenPrefix tags every token so a leaked string is recognizable (and caught
// by the same secret scanners as API keys). The trailing "m" distinguishes a
// mobile token from a machine API key at a glance.
const tokenPrefix = "so_m_"

// DefaultTTL is how long an issued token stays valid before the app must log
// in again.
const DefaultTTL = 30 * 24 * time.Hour

// ChallengeTTL bounds how long the MFA step may take after a correct password.
const ChallengeTTL = 10 * time.Minute

var (
	ErrNotFound = errors.New("mobileauth: not found")
	ErrRevoked  = errors.New("mobileauth: revoked")
	ErrExpired  = errors.New("mobileauth: expired")
)

// Token is a mobile auth token record. Secret is only populated by Create — it
// is the one and only time the raw token is available.
type Token struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	AccountID  uuid.UUID
	Prefix     string
	DeviceName string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	Secret     string // raw token — Create only, never persisted
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Create mints a token for (userID, accountID), stores its hash, and returns
// the record with Secret populated. The caller returns Secret to the app once
// and then discards it.
func (s *Store) Create(ctx context.Context, userID, accountID uuid.UUID, deviceName string, ttl time.Duration) (Token, error) {
	raw, err := generate()
	if err != nil {
		return Token{}, err
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	t := Token{
		UserID:     userID,
		AccountID:  accountID,
		Prefix:     displayPrefix(raw),
		DeviceName: deviceName,
		ExpiresAt:  time.Now().Add(ttl),
		Secret:     raw,
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO mobile_auth_tokens (user_id, account_id, token_prefix, token_hash, device_name, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at
	`, userID, accountID, t.Prefix, hash(raw), deviceName, t.ExpiresAt).Scan(&t.ID, &t.CreatedAt)
	return t, err
}

// Authenticate resolves a raw token to its (user, account). It rejects
// unknown, revoked, expired, and soft-deleted-account tokens, and best-effort
// stamps last_used_at.
func (s *Store) Authenticate(ctx context.Context, raw string) (Token, error) {
	if !strings.HasPrefix(raw, tokenPrefix) {
		return Token{}, ErrNotFound
	}
	var t Token
	err := s.pool.QueryRow(ctx, `
		SELECT t.id, t.user_id, t.account_id, t.token_prefix, t.device_name,
		       t.created_at, t.last_used_at, t.expires_at, t.revoked_at
		  FROM mobile_auth_tokens t
		  JOIN accounts a ON a.id = t.account_id
		 WHERE t.token_hash = $1 AND a.deleted_at IS NULL
	`, hash(raw)).Scan(&t.ID, &t.UserID, &t.AccountID, &t.Prefix, &t.DeviceName,
		&t.CreatedAt, &t.LastUsedAt, &t.ExpiresAt, &t.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Token{}, ErrNotFound
	}
	if err != nil {
		return Token{}, err
	}
	if t.RevokedAt != nil {
		return Token{}, ErrRevoked
	}
	if time.Now().After(t.ExpiresAt) {
		return Token{}, ErrExpired
	}
	_, _ = s.pool.Exec(ctx, `UPDATE mobile_auth_tokens SET last_used_at = now() WHERE id = $1`, t.ID)
	return t, nil
}

// List returns a user's tokens (including revoked ones), newest first.
func (s *Store) List(ctx context.Context, userID uuid.UUID) ([]Token, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, account_id, token_prefix, device_name, created_at, last_used_at, expires_at, revoked_at
		  FROM mobile_auth_tokens WHERE user_id = $1 ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.ID, &t.UserID, &t.AccountID, &t.Prefix, &t.DeviceName,
			&t.CreatedAt, &t.LastUsedAt, &t.ExpiresAt, &t.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Revoke marks a token revoked. Scoped by user so a token can only revoke
// itself / the caller's own tokens.
func (s *Store) Revoke(ctx context.Context, userID, tokenID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE mobile_auth_tokens SET revoked_at = now()
		 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL
	`, tokenID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- MFA challenges -------------------------------------------------------

// CreateChallenge records a pending MFA challenge after a correct password and
// returns its id. The app exchanges (id, code) at /mobile/mfa-verify.
func (s *Store) CreateChallenge(ctx context.Context, userID, accountID uuid.UUID, deviceName string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO mobile_mfa_challenges (user_id, account_id, device_name, expires_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, userID, accountID, deviceName, time.Now().Add(ChallengeTTL)).Scan(&id)
	return id, err
}

// Challenge is a pending MFA challenge resolved by ConsumeChallenge.
type Challenge struct {
	UserID     uuid.UUID
	AccountID  uuid.UUID
	DeviceName string
}

// ConsumeChallenge atomically deletes and returns a live challenge. A missing
// or expired challenge yields ErrNotFound / ErrExpired so it can't be reused.
func (s *Store) ConsumeChallenge(ctx context.Context, id uuid.UUID) (Challenge, error) {
	var c Challenge
	var expiresAt time.Time
	err := s.pool.QueryRow(ctx, `
		DELETE FROM mobile_mfa_challenges WHERE id = $1
		RETURNING user_id, account_id, device_name, expires_at
	`, id).Scan(&c.UserID, &c.AccountID, &c.DeviceName, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Challenge{}, ErrNotFound
	}
	if err != nil {
		return Challenge{}, err
	}
	if time.Now().After(expiresAt) {
		return Challenge{}, ErrExpired
	}
	return c, nil
}

func generate() (string, error) {
	b := make([]byte, 30)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// displayPrefix is the non-secret leading slice stored for display.
func displayPrefix(raw string) string {
	if len(raw) < 13 {
		return raw
	}
	return raw[:13]
}
