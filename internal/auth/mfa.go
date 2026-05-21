package auth

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// recoveryCodeCount is how many single-use recovery codes are minted when MFA
// is enabled.
const recoveryCodeCount = 10

// EnableMFA stores a confirmed TOTP secret and turns MFA on for the user.
func (s *Store) EnableMFA(ctx context.Context, userID uuid.UUID, secret string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE users SET totp_secret = $2, mfa_enabled = TRUE WHERE id = $1
	`, userID, secret)
	return err
}

// DisableMFA clears a user's TOTP secret and all their recovery codes.
func (s *Store) DisableMFA(ctx context.Context, userID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `
		UPDATE users SET totp_secret = NULL, mfa_enabled = FALSE WHERE id = $1
	`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM mfa_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// TOTPSecret returns a user's stored TOTP secret; empty when MFA is off.
func (s *Store) TOTPSecret(ctx context.Context, userID uuid.UUID) (string, error) {
	var secret *string
	if err := s.pool.QueryRow(ctx,
		`SELECT totp_secret FROM users WHERE id = $1`, userID).Scan(&secret); err != nil {
		return "", err
	}
	if secret == nil {
		return "", nil
	}
	return *secret, nil
}

// GenerateRecoveryCodes returns a fresh batch of human-friendly recovery
// codes (raw — show once, then discard).
func GenerateRecoveryCodes() ([]string, error) {
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	codes := make([]string, 0, recoveryCodeCount)
	for i := 0; i < recoveryCodeCount; i++ {
		b := make([]byte, 5)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		raw := strings.ToLower(enc.EncodeToString(b)) // 8 chars
		codes = append(codes, raw[:4]+"-"+raw[4:])
	}
	return codes, nil
}

// normalizeRecoveryCode strips formatting so codes match regardless of how
// the user typed the dashes/spaces/case.
func normalizeRecoveryCode(s string) string {
	return strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(s)))
}

// ReplaceRecoveryCodes swaps a user's recovery codes for a new set, storing
// only the hashes.
func (s *Store) ReplaceRecoveryCodes(ctx context.Context, userID uuid.UUID, codes []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `DELETE FROM mfa_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return err
	}
	for _, c := range codes {
		if _, err := tx.Exec(ctx, `
			INSERT INTO mfa_recovery_codes (user_id, code_hash) VALUES ($1, $2)
		`, userID, hashToken(normalizeRecoveryCode(c))); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ConsumeRecoveryCode marks a matching unused recovery code used. Returns
// true only when a previously-unused code was consumed.
func (s *Store) ConsumeRecoveryCode(ctx context.Context, userID uuid.UUID, raw string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE mfa_recovery_codes SET used_at = now()
		 WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL
	`, userID, hashToken(normalizeRecoveryCode(raw)))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// PendingMFAUser resolves the session cookie to its user only when that
// session is still awaiting an MFA code. Used by the /login/mfa challenge.
func (s *Store) PendingMFAUser(ctx context.Context, r *http.Request) (*User, error) {
	c, err := r.Cookie(s.cookieName())
	if err != nil {
		return nil, ErrNoSession
	}
	var (
		userID    uuid.UUID
		pending   bool
		expiresAt time.Time
	)
	err = s.pool.QueryRow(ctx, `
		SELECT user_id, mfa_pending, expires_at FROM sessions WHERE token_hash = $1
	`, hashToken(c.Value)).Scan(&userID, &pending, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}
	if !pending || time.Now().UTC().After(expiresAt) {
		return nil, ErrNoSession
	}
	return s.loadUser(ctx, userID)
}

// CompleteMFA clears the mfa_pending flag, promoting the session to a fully
// authenticated one.
func (s *Store) CompleteMFA(ctx context.Context, r *http.Request) error {
	c, err := r.Cookie(s.cookieName())
	if err != nil {
		return ErrNoSession
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE sessions SET mfa_pending = FALSE
		 WHERE token_hash = $1 AND mfa_pending = TRUE
	`, hashToken(c.Value))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNoSession
	}
	return nil
}
