// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package evidence

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrShredded is returned when an account's evidence key has been destroyed:
// its evidence is permanently unrecoverable (a GDPR crypto-shred).
var ErrShredded = errors.New("evidence: encryption key destroyed (crypto-shredded)")

// Cipher does per-account envelope encryption of evidence blobs. A 32-byte
// master key wraps each account's data-encryption key (DEK); the DEK in turn
// encrypts the evidence PDF. Destroying an account's wrapped DEK row
// crypto-shreds its evidence — the ciphertext can never be decrypted again,
// even if a copy of the file survives a backup or a missed deletion.
type Cipher struct {
	master    cipher.AEAD
	pool      *pgxpool.Pool
	ephemeral bool
}

// NewCipher builds a Cipher. masterKeyB64 is a base64-encoded 32-byte key
// (the EVIDENCE_ENCRYPTION_KEY config). When empty an ephemeral key is
// generated — fine for dev/CI, never for production, where evidence written
// before a restart would become permanently unrecoverable.
func NewCipher(masterKeyB64 string, pool *pgxpool.Pool) (*Cipher, error) {
	var (
		key       []byte
		ephemeral bool
	)
	if masterKeyB64 == "" {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		ephemeral = true
	} else {
		k, err := base64.StdEncoding.DecodeString(masterKeyB64)
		if err != nil {
			return nil, fmt.Errorf("evidence: EVIDENCE_ENCRYPTION_KEY is not valid base64: %w", err)
		}
		if len(k) != 32 {
			return nil, fmt.Errorf("evidence: EVIDENCE_ENCRYPTION_KEY must decode to 32 bytes, got %d", len(k))
		}
		key = k
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	return &Cipher{master: aead, pool: pool, ephemeral: ephemeral}, nil
}

// Ephemeral reports whether the master key is generated (non-persistent).
func (c *Cipher) Ephemeral() bool { return c.ephemeral }

// Encrypt seals plaintext under the account's DEK, minting the DEK on first
// use. The result is opaque (a nonce prefixed to AES-256-GCM ciphertext).
func (c *Cipher) Encrypt(ctx context.Context, accountID uuid.UUID, plaintext []byte) ([]byte, error) {
	dek, err := c.accountDEK(ctx, accountID, true)
	if err != nil {
		return nil, err
	}
	return seal(dek, plaintext)
}

// Decrypt opens a blob produced by Encrypt. It returns ErrShredded when the
// account's key has been destroyed.
func (c *Cipher) Decrypt(ctx context.Context, accountID uuid.UUID, blob []byte) ([]byte, error) {
	dek, err := c.accountDEK(ctx, accountID, false)
	if err != nil {
		return nil, err
	}
	return open(dek, blob)
}

// ShredAccount destroys an account's DEK, crypto-shredding all its evidence.
func (c *Cipher) ShredAccount(ctx context.Context, accountID uuid.UUID) error {
	_, err := c.pool.Exec(ctx, `DELETE FROM account_evidence_keys WHERE account_id = $1`, accountID)
	return err
}

// accountDEK loads (or, when create is set, lazily mints) an account's
// data-encryption key, unwrapped and ready as an AEAD.
//
// The wrapped DEK is sealed with the account's UUID as AAD: an attacker
// who can swap a wrapped_dek row between two accounts in the database can
// no longer have it decrypt successfully — the AAD mismatch on Open fails
// the GCM tag check.
func (c *Cipher) accountDEK(ctx context.Context, accountID uuid.UUID, create bool) (cipher.AEAD, error) {
	wrapped, err := c.loadWrappedDEK(ctx, accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		if !create {
			return nil, ErrShredded
		}
		if wrapped, err = c.mintDEK(ctx, accountID); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	id := accountID
	raw, err := openAAD(c.master, wrapped, id[:])
	if err != nil {
		return nil, fmt.Errorf("evidence: unwrap account key: %w", err)
	}
	return newAEAD(raw)
}

func (c *Cipher) loadWrappedDEK(ctx context.Context, accountID uuid.UUID) ([]byte, error) {
	var w []byte
	err := c.pool.QueryRow(ctx,
		`SELECT wrapped_dek FROM account_evidence_keys WHERE account_id = $1`, accountID).Scan(&w)
	return w, err
}

// mintDEK generates a fresh DEK, wraps it under the master key, and stores
// it. Concurrent first-writes race via ON CONFLICT; the re-select returns
// whichever row won, so an account's DEK is stable once set.
func (c *Cipher) mintDEK(ctx context.Context, accountID uuid.UUID) ([]byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	id := accountID
	wrapped, err := sealAAD(c.master, raw, id[:])
	if err != nil {
		return nil, err
	}
	if _, err := c.pool.Exec(ctx, `
		INSERT INTO account_evidence_keys (account_id, wrapped_dek)
		VALUES ($1, $2) ON CONFLICT (account_id) DO NOTHING
	`, accountID, wrapped); err != nil {
		return nil, err
	}
	return c.loadWrappedDEK(ctx, accountID)
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// seal encrypts plaintext, returning nonce||ciphertext.
func seal(aead cipher.AEAD, plaintext []byte) ([]byte, error) {
	return sealAAD(aead, plaintext, nil)
}

// sealAAD encrypts plaintext with the given AAD, returning nonce||ciphertext.
// AAD binds the ciphertext to that context (e.g. account UUID); decrypt with
// the same AAD or the GCM tag check fails.
func sealAAD(aead cipher.AEAD, plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, aad), nil
}

// open reverses seal.
func open(aead cipher.AEAD, blob []byte) ([]byte, error) {
	return openAAD(aead, blob, nil)
}

// openAAD reverses sealAAD; the AAD must match what was used during seal.
func openAAD(aead cipher.AEAD, blob, aad []byte) ([]byte, error) {
	if len(blob) < aead.NonceSize() {
		return nil, errors.New("evidence: ciphertext too short")
	}
	nonce, ct := blob[:aead.NonceSize()], blob[aead.NonceSize():]
	return aead.Open(nil, nonce, ct, aad)
}
