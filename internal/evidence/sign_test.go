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
