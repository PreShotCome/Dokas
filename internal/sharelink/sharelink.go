// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

// Package sharelink issues and validates tokenized read-only share URLs for a
// single drill. An account owner mints one; an auditor (or the person who
// needs to see the receipt) opens the URL to view the drill's mono receipt +
// signature and download the signed PDF, without needing a Dokaz account.
//
// Tokens are 32 random bytes, presented as sh_<44-char base64url>, and
// SHA-256-hashed at rest. The exchange is bearer-only: possession = access.
package sharelink

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// tokenPrefix is the client-visible prefix on every share token. The prefix
// tells log scrubbers "this is a Dokaz share token, redact it" — same shape
// as apikey.so_ / mobileauth.so_m_ tokens for consistency.
const tokenPrefix = "sh_"

// DefaultTTL is the horizon the UI defaults to when minting a link (30 days).
// Long enough for a typical SOC 2 audit cycle; short enough that a forgotten
// link doesn't outlive the drill's relevance.
const DefaultTTL = 30 * 24 * time.Hour

// ErrNotFound covers both a missing row and an expired/revoked link. The
// bearer sees the same message for either case — no leaked signal.
var ErrNotFound = errors.New("sharelink: link not found or no longer valid")

// Link is the row shape stored per share link.
type Link struct {
	ID           uuid.UUID
	DrillID      uuid.UUID
	AccountID    uuid.UUID
	CreatedByID  uuid.UUID
	TokenPrefix  string
	Label        string
	ExpiresAt    time.Time
	RevokedAt    *time.Time
	LastViewedAt *time.Time
	ViewCount    int
	CreatedAt    time.Time
}

// Active reports whether the link is currently usable — not revoked, not past
// the expiry horizon.
func (l Link) Active(now time.Time) bool {
	if l.RevokedAt != nil {
		return false
	}
	return now.Before(l.ExpiresAt)
}

// Minted is a fresh link plus its one-time cleartext token — returned to the
// caller who minted it and never persisted. The token is what the auditor
// puts in their URL bar.
type Minted struct {
	Link  Link
	Token string
}

// Store persists share links.
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Create mints a new share link for the given drill. Only the returned Token
// can dereference the link; the DB stores only its SHA-256 hash.
func (s *Store) Create(ctx context.Context, drillID, accountID, createdBy uuid.UUID, label string, ttl time.Duration) (Minted, error) {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	tok, err := newToken()
	if err != nil {
		return Minted{}, err
	}
	hash := sha256.Sum256([]byte(tok))
	prefix := tok[:len(tokenPrefix)+6] // sh_ + 6 chars of body, non-secret
	expiresAt := time.Now().UTC().Add(ttl)

	var l Link
	err = s.pool.QueryRow(ctx, `
		INSERT INTO drill_share_links (drill_id, account_id, created_by, token_hash, token_prefix, label, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, drill_id, account_id, created_by, token_prefix, label, expires_at, revoked_at, last_viewed_at, view_count, created_at
	`, drillID, accountID, createdBy, hash[:], prefix, label, expiresAt).Scan(
		&l.ID, &l.DrillID, &l.AccountID, &l.CreatedByID, &l.TokenPrefix, &l.Label,
		&l.ExpiresAt, &l.RevokedAt, &l.LastViewedAt, &l.ViewCount, &l.CreatedAt)
	if err != nil {
		return Minted{}, err
	}
	return Minted{Link: l, Token: tok}, nil
}

// Resolve looks up a link by its cleartext token, gates on active status, and
// records the view. Returns ErrNotFound if the token is unknown, expired, or
// revoked — the caller must not distinguish the reasons.
func (s *Store) Resolve(ctx context.Context, token string) (Link, error) {
	if !strings.HasPrefix(token, tokenPrefix) {
		return Link{}, ErrNotFound
	}
	hash := sha256.Sum256([]byte(token))
	var l Link
	err := s.pool.QueryRow(ctx, `
		SELECT id, drill_id, account_id, created_by, token_prefix, label,
		       expires_at, revoked_at, last_viewed_at, view_count, created_at
		  FROM drill_share_links
		 WHERE token_hash = $1
	`, hash[:]).Scan(
		&l.ID, &l.DrillID, &l.AccountID, &l.CreatedByID, &l.TokenPrefix, &l.Label,
		&l.ExpiresAt, &l.RevokedAt, &l.LastViewedAt, &l.ViewCount, &l.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Link{}, ErrNotFound
	}
	if err != nil {
		return Link{}, err
	}
	if !l.Active(time.Now().UTC()) {
		return Link{}, ErrNotFound
	}
	// Best-effort view metric. A write failure never denies access — the
	// primary read has already resolved the link.
	_, _ = s.pool.Exec(ctx, `
		UPDATE drill_share_links
		   SET last_viewed_at = now(), view_count = view_count + 1
		 WHERE id = $1
	`, l.ID)
	return l, nil
}

// ListForDrill returns every link ever minted for a drill, newest first, so
// the account owner can inspect / revoke.
func (s *Store) ListForDrill(ctx context.Context, drillID uuid.UUID) ([]Link, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, drill_id, account_id, created_by, token_prefix, label,
		       expires_at, revoked_at, last_viewed_at, view_count, created_at
		  FROM drill_share_links
		 WHERE drill_id = $1
		 ORDER BY created_at DESC
	`, drillID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Link
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.ID, &l.DrillID, &l.AccountID, &l.CreatedByID, &l.TokenPrefix, &l.Label,
			&l.ExpiresAt, &l.RevokedAt, &l.LastViewedAt, &l.ViewCount, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Revoke marks a link revoked so future Resolve calls return ErrNotFound. Only
// callable by the account owner (enforced at the handler layer).
func (s *Store) Revoke(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE drill_share_links SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, id)
	return err
}

// newToken returns a fresh sh_<base64url> token.
func newToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(b[:]), nil
}
