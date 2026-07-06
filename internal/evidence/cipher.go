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
	active    cipher.AEAD   // current master key: wraps new DEKs, target of re-wrap
	retired   []cipher.AEAD // prior master keys, tried on unwrap to enable rotation
	pool      *pgxpool.Pool
	ephemeral bool
}

// NewCipher builds a Cipher. activeKeyB64 is the base64-encoded 32-byte master
// key (EVIDENCE_ENCRYPTION_KEY). retiredKeysB64 are previously-active master
// keys (EVIDENCE_ENCRYPTION_KEYS_RETIRED) retained only so DEKs sealed under
// them during a rotation still unwrap; such a DEK is lazily re-wrapped under
// the active key on first access. When activeKeyB64 is empty an ephemeral key
// is generated — fine for dev/CI, never for production, where evidence written
// before a restart would become permanently unrecoverable.
func NewCipher(activeKeyB64 string, retiredKeysB64 []string, pool *pgxpool.Pool) (*Cipher, error) {
	active, ephemeral, err := loadMasterKey(activeKeyB64, "EVIDENCE_ENCRYPTION_KEY")
	if err != nil {
		return nil, err
	}
	var retired []cipher.AEAD
	for i, b := range retiredKeysB64 {
		if b == "" {
			continue
		}
		aead, _, err := loadMasterKey(b, fmt.Sprintf("EVIDENCE_ENCRYPTION_KEYS_RETIRED[%d]", i))
		if err != nil {
			return nil, err
		}
		retired = append(retired, aead)
	}
	return &Cipher{active: active, retired: retired, pool: pool, ephemeral: ephemeral}, nil
}

// loadMasterKey decodes a base64 32-byte master key into an AEAD. An empty
// string yields a fresh ephemeral key (ephemeral=true).
func loadMasterKey(b64, name string) (cipher.AEAD, bool, error) {
	if b64 == "" {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, false, err
		}
		aead, err := newAEAD(key)
		return aead, true, err
	}
	k, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, false, fmt.Errorf("evidence: %s is not valid base64: %w", name, err)
	}
	if len(k) != 32 {
		return nil, false, fmt.Errorf("evidence: %s must decode to 32 bytes, got %d", name, len(k))
	}
	aead, err := newAEAD(k)
	return aead, false, err
}

// Ephemeral reports whether the active master key is generated (non-persistent).
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
	raw, err := c.unwrapDEK(ctx, accountID, wrapped)
	if err != nil {
		return nil, err
	}
	return newAEAD(raw)
}

// unwrapDEK opens a wrapped DEK, trying the active master key first and then
// each retired key. A DEK that only a retired key opens is lazily re-wrapped
// under the active key (best-effort — a failed re-wrap never fails the caller),
// so a master-key rotation migrates rows as they are accessed. When no key
// opens it, the original active-key error is returned (unrecoverable evidence).
func (c *Cipher) unwrapDEK(ctx context.Context, accountID uuid.UUID, wrapped []byte) ([]byte, error) {
	id := accountID
	raw, activeErr := openAAD(c.active, wrapped, id[:])
	if activeErr == nil {
		return raw, nil
	}
	for _, r := range c.retired {
		raw, err := openAAD(r, wrapped, id[:])
		if err != nil {
			continue
		}
		if rewrapped, e := sealAAD(c.active, raw, id[:]); e == nil {
			_, _ = c.pool.Exec(ctx,
				`UPDATE account_evidence_keys SET wrapped_dek = $2 WHERE account_id = $1`,
				accountID, rewrapped)
		}
		return raw, nil
	}
	return nil, fmt.Errorf("evidence: unwrap account key: %w", activeErr)
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
	wrapped, err := sealAAD(c.active, raw, id[:])
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

// --- master-key rotation ops (driven by cmd/evidence-keys) ---

// KeyAudit classifies every account's wrapped DEK against the configured keys.
// Active unwraps with the current key; Retired unwraps only with a retired key
// (a rotation is mid-flight and these rows should be re-wrapped); Poison rows
// unwrap with no configured key — their evidence is permanently unrecoverable.
type KeyAudit struct {
	Active  int
	Retired int
	Poison  []uuid.UUID
}

// classify reports which configured key, if any, unwraps a DEK.
func (c *Cipher) classify(accountID uuid.UUID, wrapped []byte) string {
	id := accountID
	if _, err := openAAD(c.active, wrapped, id[:]); err == nil {
		return "active"
	}
	for _, r := range c.retired {
		if _, err := openAAD(r, wrapped, id[:]); err == nil {
			return "retired"
		}
	}
	return "poison"
}

// forEachWrapped streams every (account_id, wrapped_dek) row to fn.
func (c *Cipher) forEachWrapped(ctx context.Context, fn func(uuid.UUID, []byte) error) error {
	rows, err := c.pool.Query(ctx, `SELECT account_id, wrapped_dek FROM account_evidence_keys`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var w []byte
		if err := rows.Scan(&id, &w); err != nil {
			return err
		}
		if err := fn(id, w); err != nil {
			return err
		}
	}
	return rows.Err()
}

// Audit classifies every account's wrapped DEK without modifying anything.
func (c *Cipher) Audit(ctx context.Context) (KeyAudit, error) {
	var a KeyAudit
	err := c.forEachWrapped(ctx, func(id uuid.UUID, w []byte) error {
		switch c.classify(id, w) {
		case "active":
			a.Active++
		case "retired":
			a.Retired++
		default:
			a.Poison = append(a.Poison, id)
		}
		return nil
	})
	return a, err
}

// RewrapAll re-wraps every DEK that a retired key opens under the active key,
// completing a rotation eagerly instead of waiting for lazy per-access
// migration. Rows already on the active key are skipped; poison rows are left
// untouched and counted. Returns (rewrapped, poison).
func (c *Cipher) RewrapAll(ctx context.Context) (int, int, error) {
	var rewrapped, poison int
	err := c.forEachWrapped(ctx, func(id uuid.UUID, w []byte) error {
		switch c.classify(id, w) {
		case "active":
			return nil
		case "poison":
			poison++
			return nil
		}
		// Opens with a retired key — re-wrap under the active key.
		var raw []byte
		for _, r := range c.retired {
			if got, err := openAAD(r, w, id[:]); err == nil {
				raw = got
				break
			}
		}
		sealed, err := sealAAD(c.active, raw, id[:])
		if err != nil {
			return err
		}
		if _, err := c.pool.Exec(ctx,
			`UPDATE account_evidence_keys SET wrapped_dek = $2 WHERE account_id = $1`, id, sealed); err != nil {
			return err
		}
		rewrapped++
		return nil
	})
	return rewrapped, poison, err
}

// ShredPoison deletes every DEK row that no configured key can unwrap. Their
// evidence is already permanently unrecoverable, so the dead row is only
// blocking a fresh DEK from minting. Returns the number of rows deleted.
func (c *Cipher) ShredPoison(ctx context.Context) (int, error) {
	audit, err := c.Audit(ctx)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, id := range audit.Poison {
		if _, err := c.pool.Exec(ctx, `DELETE FROM account_evidence_keys WHERE account_id = $1`, id); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}
