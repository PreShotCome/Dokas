// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package evidence

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func randKeyB64(t *testing.T) string {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return base64.StdEncoding.EncodeToString(k)
}

func mkAccount(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id, name, slug) VALUES ($1,'rot','rot-'||$2)`,
		id, id.String()[:8]); err != nil {
		t.Fatalf("account: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, id) })
	return id
}

// TestMasterKeyRotation covers the evidence master-key lifecycle: a DEK sealed
// under an old key still opens after rotation (via a retired key), is lazily
// re-wrapped under the active key on access, and RewrapAll / ShredPoison behave.
func TestMasterKeyRotation(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	keyA, keyB := randKeyB64(t), randKeyB64(t)
	acct := mkAccount(t, ctx, pool)

	// Seal a blob under key A.
	cA, _ := NewCipher(keyA, nil, pool)
	blob, err := cA.Encrypt(ctx, acct, []byte("evidence"))
	if err != nil {
		t.Fatalf("encrypt under A: %v", err)
	}

	// Key B alone cannot open it — the DEK is poison to B; with A retired it
	// only opens via the retired key.
	cBonly, _ := NewCipher(keyB, nil, pool)
	cRot, _ := NewCipher(keyB, []string{keyA}, pool)
	if _, err := cBonly.Decrypt(ctx, acct, blob); err == nil {
		t.Fatal("decrypt under B alone should fail (wrong master key)")
	}
	if got := classifyAcct(t, ctx, cBonly, acct); got != "poison" {
		t.Fatalf("acct classify under B alone = %q, want poison", got)
	}
	if got := classifyAcct(t, ctx, cRot, acct); got != "retired" {
		t.Fatalf("acct classify under B+retired-A = %q, want retired", got)
	}

	// Rotate to B with A retired: decrypt works AND lazily re-wraps to B.
	got, err := cRot.Decrypt(ctx, acct, blob)
	if err != nil || string(got) != "evidence" {
		t.Fatalf("decrypt during rotation: got %q err %v", got, err)
	}
	// After that access the row must open under B alone (migration persisted).
	if _, err := cBonly.Decrypt(ctx, acct, blob); err != nil {
		t.Fatalf("after lazy re-wrap, B alone should decrypt: %v", err)
	}
	if got := classifyAcct(t, ctx, cBonly, acct); got != "active" {
		t.Fatalf("acct classify after migration = %q, want active", got)
	}

	// RewrapAll: a second account still on A migrates to B eagerly.
	acct2 := mkAccount(t, ctx, pool)
	if _, err := cA.Encrypt(ctx, acct2, []byte("x")); err != nil {
		t.Fatalf("encrypt acct2 under A: %v", err)
	}
	if got := classifyAcct(t, ctx, cRot, acct2); got != "retired" {
		t.Fatalf("acct2 classify before rewrap = %q, want retired", got)
	}
	// RewrapAll spans the whole table (shared test DB), so only assert it made
	// progress; it must migrate acct2 to the active key.
	if rew, _, err := cRot.RewrapAll(ctx); err != nil || rew < 1 {
		t.Fatalf("RewrapAll: rewrapped=%d err=%v (want >=1)", rew, err)
	}
	if got := classifyAcct(t, ctx, cBonly, acct2); got != "active" {
		t.Fatalf("acct2 classify after RewrapAll = %q, want active", got)
	}

	// ShredPoison: a DEK wrapped under an unrelated key X is poison and deleted.
	acct3 := mkAccount(t, ctx, pool)
	kx, _ := base64.StdEncoding.DecodeString(randKeyB64(t))
	aeadX, _ := newAEAD(kx)
	dek := make([]byte, 32)
	_, _ = rand.Read(dek)
	id3 := acct3
	wrapped, _ := sealAAD(aeadX, dek, id3[:])
	if _, err := pool.Exec(ctx, `INSERT INTO account_evidence_keys (account_id, wrapped_dek) VALUES ($1,$2)`, acct3, wrapped); err != nil {
		t.Fatalf("insert poison dek: %v", err)
	}
	if got := classifyAcct(t, ctx, cRot, acct3); got != "poison" {
		t.Fatalf("acct3 classify = %q, want poison", got)
	}
	if _, err := cRot.ShredPoison(ctx); err != nil {
		t.Fatalf("ShredPoison: %v", err)
	}
	// acct3's poison row is gone; acct/acct2 (now active) are untouched.
	if _, err := cRot.loadWrappedDEK(ctx, acct3); err == nil {
		t.Fatal("acct3 poison row should be deleted by ShredPoison")
	}
	if got := classifyAcct(t, ctx, cBonly, acct); got != "active" {
		t.Fatalf("acct must survive ShredPoison as active, got %q", got)
	}
}

// classifyAcct loads an account's wrapped DEK and classifies it against c's keys.
func classifyAcct(t *testing.T, ctx context.Context, c *Cipher, acct uuid.UUID) string {
	t.Helper()
	w, err := c.loadWrappedDEK(ctx, acct)
	if err != nil {
		t.Fatalf("load dek for %s: %v", acct, err)
	}
	return c.classify(acct, w)
}
