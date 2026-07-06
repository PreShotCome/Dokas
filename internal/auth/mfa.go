// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

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
// confirmCounter is the TOTP time-step the enrollment code matched at — it
// is written to totp_last_used_counter so the same code cannot be replayed
// at /login/mfa within its ~90s validity window.
func (s *Store) EnableMFA(ctx context.Context, userID uuid.UUID, secret string, confirmCounter int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE users
		   SET totp_secret = $2,
		       mfa_enabled = TRUE,
		       totp_last_used_counter = $3
		 WHERE id = $1
	`, userID, secret, confirmCounter)
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

// VerifyAndConsumeTOTP verifies a login TOTP code and records its time-step
// counter so the same code cannot be replayed within its ~90s validity
// window. The check and the counter write are one locked transaction, so two
// concurrent submissions of the same code cannot both succeed.
func (s *Store) VerifyAndConsumeTOTP(ctx context.Context, userID uuid.UUID, code string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		secret *string
		last   *int64
	)
	err = tx.QueryRow(ctx, `
		SELECT totp_secret, totp_last_used_counter FROM users WHERE id = $1 FOR UPDATE
	`, userID).Scan(&secret, &last)
	if err != nil {
		return false, err
	}
	if secret == nil {
		return false, nil
	}
	counter, ok := VerifyTOTPWithCounter(*secret, code, time.Now())
	if !ok {
		return false, nil
	}
	if last != nil && counter <= *last {
		return false, nil // replay of an already-used (or older) code
	}
	if _, err := tx.Exec(ctx,
		`UPDATE users SET totp_last_used_counter = $2 WHERE id = $1`, userID, counter); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
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

// CompleteMFA finishes the second login step: it destroys the pending
// session and issues a fresh, fully authenticated one with a new token. The
// session is rotated at the auth boundary, so a cookie observed during the
// pending window does not carry over into the authenticated session.
func (s *Store) CompleteMFA(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	c, err := r.Cookie(s.cookieName())
	if err != nil {
		return ErrNoSession
	}
	hash := hashToken(c.Value)

	var (
		userID  uuid.UUID
		pending bool
	)
	err = s.pool.QueryRow(ctx, `
		SELECT user_id, mfa_pending FROM sessions WHERE token_hash = $1
	`, hash).Scan(&userID, &pending)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoSession
	}
	if err != nil {
		return err
	}
	if !pending {
		return ErrNoSession
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, hash); err != nil {
		return err
	}
	return s.create(ctx, w, userID, uuid.Nil, false)
}
