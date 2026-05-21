// Package apikey issues and verifies API keys for the /v1 REST API. Keys
// authenticate machine clients; only a SHA-256 hash is stored, so a database
// leak doesn't expose usable keys.
package apikey

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

// keyPrefix tags every key so a leaked string is recognizable as a Restore
// Drill credential (helps secret scanners).
const keyPrefix = "rd_"

var (
	ErrNotFound = errors.New("apikey: not found")
	ErrRevoked  = errors.New("apikey: revoked")
)

// Key is an API key record. Secret is only populated by Create — it is the
// one and only time the raw key is available.
type Key struct {
	ID              uuid.UUID
	AccountID       uuid.UUID
	Name            string
	Prefix          string
	CreatedByUserID uuid.UUID
	CreatedAt       time.Time
	LastUsedAt      *time.Time
	RevokedAt       *time.Time
	Secret          string // raw key — Create only, never persisted
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Create generates a new key, stores its hash, and returns the record with
// the raw Secret populated. The caller must show Secret to the user once and
// then discard it.
func (s *Store) Create(ctx context.Context, accountID, createdBy uuid.UUID, name string) (Key, error) {
	raw, err := generate()
	if err != nil {
		return Key{}, err
	}
	k := Key{
		AccountID: accountID,
		Name:      name,
		Prefix:    displayPrefix(raw),
		Secret:    raw,
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO api_keys (account_id, name, key_prefix, key_hash, created_by_user_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`, accountID, name, k.Prefix, hash(raw), createdBy).Scan(&k.ID, &k.CreatedAt)
	return k, err
}

// Authenticate resolves a raw key to its account. It rejects unknown and
// revoked keys, and best-effort updates last_used_at.
func (s *Store) Authenticate(ctx context.Context, raw string) (Key, error) {
	if !strings.HasPrefix(raw, keyPrefix) {
		return Key{}, ErrNotFound
	}
	var k Key
	err := s.pool.QueryRow(ctx, `
		SELECT id, account_id, name, key_prefix, created_by_user_id, created_at, last_used_at, revoked_at
		  FROM api_keys WHERE key_hash = $1
	`, hash(raw)).Scan(&k.ID, &k.AccountID, &k.Name, &k.Prefix,
		&k.CreatedByUserID, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Key{}, ErrNotFound
	}
	if err != nil {
		return Key{}, err
	}
	if k.RevokedAt != nil {
		return Key{}, ErrRevoked
	}
	_, _ = s.pool.Exec(ctx, `UPDATE api_keys SET last_used_at = now() WHERE id = $1`, k.ID)
	return k, nil
}

// List returns an account's keys (including revoked ones), newest first.
func (s *Store) List(ctx context.Context, accountID uuid.UUID) ([]Key, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, account_id, name, key_prefix, created_by_user_id, created_at, last_used_at, revoked_at
		  FROM api_keys WHERE account_id = $1 ORDER BY created_at DESC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Key
	for rows.Next() {
		var k Key
		if err := rows.Scan(&k.ID, &k.AccountID, &k.Name, &k.Prefix,
			&k.CreatedByUserID, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// Revoke marks a key revoked. Account-scoped so one account can't revoke
// another's key.
func (s *Store) Revoke(ctx context.Context, accountID, keyID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE api_keys SET revoked_at = now()
		 WHERE id = $1 AND account_id = $2 AND revoked_at IS NULL
	`, keyID, accountID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func generate() (string, error) {
	b := make([]byte, 30)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return keyPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// displayPrefix is the non-secret leading slice shown in the UI / key list.
func displayPrefix(raw string) string {
	if len(raw) < 12 {
		return raw
	}
	return raw[:12]
}
