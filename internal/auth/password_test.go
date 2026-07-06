// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package auth

import (
	"strings"
	"testing"
)

func TestHashPassword_RoundTrip(t *testing.T) {
	hash, err := HashPassword("correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("expected argon2id prefix, got %q", hash)
	}
	if err := VerifyPassword("correcthorsebatterystaple", hash); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerifyPassword_Mismatch(t *testing.T) {
	hash, err := HashPassword("right")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := VerifyPassword("wrong", hash); err != ErrMismatch {
		t.Fatalf("expected mismatch, got %v", err)
	}
}

func TestVerifyPassword_InvalidHash(t *testing.T) {
	if err := VerifyPassword("anything", "not-a-hash"); err != ErrInvalidHash {
		t.Fatalf("expected invalid hash, got %v", err)
	}
}

func TestHashPassword_Uniqueness(t *testing.T) {
	// Different salts must produce different hashes for the same password.
	a, _ := HashPassword("same")
	b, _ := HashPassword("same")
	if a == b {
		t.Fatal("two hashes of the same password must differ (salt randomness)")
	}
}
