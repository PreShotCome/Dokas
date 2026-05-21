package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// MagicLinkTTL is how long a passwordless sign-in link stays valid — short,
// like a password-reset token.
const MagicLinkTTL = 15 * time.Minute

var (
	// ErrMagicLinkInvalid is returned for an unknown magic-link token.
	ErrMagicLinkInvalid = errors.New("auth: magic link not found")
	// ErrMagicLinkGone is returned for an expired or already-used token.
	ErrMagicLinkGone = errors.New("auth: magic link expired or used")
)

// CreateMagicLinkToken issues a fresh passwordless sign-in token for a user
// and returns the raw token — only its hash is persisted.
func (s *Store) CreateMagicLinkToken(ctx context.Context, userID uuid.UUID) (string, error) {
	raw, hash, err := generateToken()
	if err != nil {
		return "", err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO magic_link_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
	`, userID, hash, time.Now().UTC().Add(MagicLinkTTL))
	if err != nil {
		return "", err
	}
	return raw, nil
}

// ConsumeMagicLink marks a magic-link token used and returns its user. A
// token that is unknown, expired, or already used is rejected.
func (s *Store) ConsumeMagicLink(ctx context.Context, rawToken string) (uuid.UUID, error) {
	hash := hashToken(rawToken)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		id         uuid.UUID
		userID     uuid.UUID
		expiresAt  time.Time
		consumedAt *time.Time
	)
	err = tx.QueryRow(ctx, `
		SELECT id, user_id, expires_at, consumed_at
		  FROM magic_link_tokens
		 WHERE token_hash = $1 FOR UPDATE
	`, hash).Scan(&id, &userID, &expiresAt, &consumedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrMagicLinkInvalid
	}
	if err != nil {
		return uuid.Nil, err
	}
	if consumedAt != nil || time.Now().UTC().After(expiresAt) {
		return uuid.Nil, ErrMagicLinkGone
	}

	if _, err := tx.Exec(ctx, `
		UPDATE magic_link_tokens SET consumed_at = now() WHERE id = $1
	`, id); err != nil {
		return uuid.Nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}
