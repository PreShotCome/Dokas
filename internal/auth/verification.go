package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// VerificationTTL is how long an email-verification link stays valid.
const VerificationTTL = 24 * time.Hour

var (
	// ErrVerificationInvalid is returned for an unknown verification token.
	ErrVerificationInvalid = errors.New("auth: verification token not found")
	// ErrVerificationGone is returned for an expired or already-used token.
	ErrVerificationGone = errors.New("auth: verification token expired or used")
)

// CreateVerificationToken issues a fresh email-verification token for a user
// and returns the raw token — only its hash is persisted.
func (s *Store) CreateVerificationToken(ctx context.Context, userID uuid.UUID) (string, error) {
	raw, hash, err := generateToken()
	if err != nil {
		return "", err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO email_verification_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
	`, userID, hash, time.Now().UTC().Add(VerificationTTL))
	if err != nil {
		return "", err
	}
	return raw, nil
}

// VerifyEmail consumes a verification token: in one transaction it marks the
// token used and the user's email verified, returning the user's ID. A token
// that is unknown, expired, or already used is rejected.
func (s *Store) VerifyEmail(ctx context.Context, rawToken string) (uuid.UUID, error) {
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
		  FROM email_verification_tokens
		 WHERE token_hash = $1 FOR UPDATE
	`, hash).Scan(&id, &userID, &expiresAt, &consumedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrVerificationInvalid
	}
	if err != nil {
		return uuid.Nil, err
	}
	if consumedAt != nil || time.Now().UTC().After(expiresAt) {
		return uuid.Nil, ErrVerificationGone
	}

	if _, err := tx.Exec(ctx, `
		UPDATE email_verification_tokens SET consumed_at = now() WHERE id = $1
	`, id); err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE users SET email_verified = TRUE WHERE id = $1
	`, userID); err != nil {
		return uuid.Nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}
