// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package evidence

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"
)

func TestSignAndVerifyRoundTrip(t *testing.T) {
	signer, err := NewSigner("")
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	pdf := []byte("%PDF-1.3 ... pretend evidence ...")
	sig := signer.Sign(pdf, time.Now().UTC())

	if err := Verify(signer.PublicKey(), pdf, sig); err != nil {
		t.Fatalf("Verify on a fresh signature failed: %v", err)
	}
}

func TestVerifyDetectsTamperedPDF(t *testing.T) {
	signer, _ := NewSigner("")
	pdf := []byte("original evidence")
	sig := signer.Sign(pdf, time.Now().UTC())

	tampered := []byte("original evidence (edited)")
	if err := Verify(signer.PublicKey(), tampered, sig); err == nil {
		t.Fatal("Verify accepted a tampered PDF")
	}
}

func TestVerifyDetectsWrongKey(t *testing.T) {
	signer, _ := NewSigner("")
	pdf := []byte("evidence")
	sig := signer.Sign(pdf, time.Now().UTC())

	otherPub, _, _ := ed25519.GenerateKey(nil)
	if err := Verify(otherPub, pdf, sig); err == nil {
		t.Fatal("Verify accepted a signature under the wrong key")
	}
}

func TestVerifyDetectsTamperedTimestamp(t *testing.T) {
	signer, _ := NewSigner("")
	pdf := []byte("evidence")
	signedAt := time.Now().UTC()
	sig := signer.Sign(pdf, signedAt)

	// Move the claimed signing time — the signature covers it, so verify fails.
	sig.SignedAt = signedAt.Add(time.Hour)
	if err := Verify(signer.PublicKey(), pdf, sig); err == nil {
		t.Fatal("Verify accepted a signature with an altered timestamp")
	}
}

func TestSignerLoadsPEMKey(t *testing.T) {
	// Generate a key, marshal to PKCS#8 PEM, load it back.
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))

	signer, err := NewSigner(keyPEM)
	if err != nil {
		t.Fatalf("NewSigner(PEM): %v", err)
	}
	if signer.Ephemeral() {
		t.Fatal("signer loaded from PEM should not report Ephemeral")
	}

	// A signature from the PEM-loaded signer verifies, and survives a second
	// load of the same key — the property a persistent prod key needs.
	pdf := []byte("evidence")
	sig := signer.Sign(pdf, time.Now().UTC())
	signer2, _ := NewSigner(keyPEM)
	if err := Verify(signer2.PublicKey(), pdf, sig); err != nil {
		t.Fatalf("signature did not verify across signer reload: %v", err)
	}
}

func TestEphemeralFlag(t *testing.T) {
	signer, _ := NewSigner("")
	if !signer.Ephemeral() {
		t.Fatal("a signer with no key should report Ephemeral")
	}
}

func TestSignerVerifiesRetiredKey(t *testing.T) {
	// An "old" signer signs evidence before a key rotation.
	oldPub, oldPriv, _ := ed25519.GenerateKey(nil)
	oldSigner, err := NewSigner(pkcs8PEM(t, oldPriv))
	if err != nil {
		t.Fatalf("old signer: %v", err)
	}
	pdf := []byte("evidence signed before the rotation")
	sig := oldSigner.Sign(pdf, time.Now().UTC())

	// Rotation: a new active key, with the old public key kept for verifying.
	_, newPriv, _ := ed25519.GenerateKey(nil)
	rotated, err := NewSignerWithVerificationKeys(pkcs8PEM(t, newPriv), pkixPEM(t, oldPub))
	if err != nil {
		t.Fatalf("rotated signer: %v", err)
	}

	// The rotated signer still resolves and verifies the pre-rotation signature.
	pub, ok := rotated.VerificationKey(sig.PublicKeyID)
	if !ok {
		t.Fatal("rotated signer should hold the retired key")
	}
	if err := Verify(pub, pdf, sig); err != nil {
		t.Fatalf("evidence signed before rotation should still verify: %v", err)
	}

	// Its own active key is in the verification set too.
	if _, ok := rotated.VerificationKey(rotated.PublicKeyID()); !ok {
		t.Fatal("rotated signer should hold its own active key")
	}

	// A signer that never knew the old key cannot resolve it.
	if fresh, _ := NewSigner(""); func() bool { _, ok := fresh.VerificationKey(sig.PublicKeyID); return ok }() {
		t.Fatal("an unrelated signer must not hold the retired key")
	}
}

func TestVerificationKeysRejectsBadPEM(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	bad := "-----BEGIN PUBLIC KEY-----\nbm90LWEta2V5\n-----END PUBLIC KEY-----\n"
	if _, err := NewSignerWithVerificationKeys(pkcs8PEM(t, priv), bad); err == nil {
		t.Fatal("a malformed verification key block should be rejected")
	}
}

func pkcs8PEM(t *testing.T, priv ed25519.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func pkixPEM(t *testing.T, pub ed25519.PublicKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}
