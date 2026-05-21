// Package evidence handles tamper-evidence for drill reports: detached
// signatures, an evidence store abstraction, and retention metadata.
//
// Production deployments inject a real document-signing key (and, later, a
// cert chain + RFC 3161 timestamp authority). Locally the signer falls back
// to an ephemeral key so dev and CI work without secrets — the signing and
// verification machinery is identical either way.
package evidence

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"time"
)

// Signature is a detached signature over a drill's evidence PDF.
type Signature struct {
	Algorithm   string // always "ed25519" in this phase
	PublicKeyID string // sha256 of the public key, hex — the "key fingerprint"
	Value       string // base64 signature
	PDFSHA256   string // hex digest the signature covers
	SignedAt    time.Time
}

// Signer produces detached signatures. It is safe for concurrent use.
type Signer struct {
	priv        ed25519.PrivateKey
	publicKeyID string
	ephemeral   bool
}

// NewSigner builds a Signer. keyPEM is a PKCS#8 Ed25519 private key in PEM
// form (the EVIDENCE_SIGNING_KEY config value). When keyPEM is empty an
// ephemeral key is generated — fine for dev/CI, never for production
// evidence that must verify across restarts.
func NewSigner(keyPEM string) (*Signer, error) {
	if keyPEM == "" {
		pub, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			return nil, fmt.Errorf("generate ephemeral key: %w", err)
		}
		return &Signer{priv: priv, publicKeyID: keyID(pub), ephemeral: true}, nil
	}

	block, _ := pem.Decode([]byte(keyPEM))
	if block == nil {
		return nil, errors.New("evidence: EVIDENCE_SIGNING_KEY is not valid PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse signing key: %w", err)
	}
	priv, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("evidence: signing key is %T, want ed25519", parsed)
	}
	pub := priv.Public().(ed25519.PublicKey)
	return &Signer{priv: priv, publicKeyID: keyID(pub)}, nil
}

// Ephemeral reports whether the signer is using a generated (non-persistent)
// key. Callers log a warning so a misconfigured production deploy is loud.
func (s *Signer) Ephemeral() bool { return s.ephemeral }

// PublicKeyID is the fingerprint clients use to look up the verifying key.
func (s *Signer) PublicKeyID() string { return s.publicKeyID }

// PublicKey returns the raw verifying key.
func (s *Signer) PublicKey() ed25519.PublicKey {
	return s.priv.Public().(ed25519.PublicKey)
}

// Sign produces a detached signature over the PDF bytes. The signed message
// is sha256(pdf) ‖ signedAt(RFC3339Nano) so the signature also attests the
// time — a self-asserted trusted timestamp. A real RFC 3161 token plugs in
// at this seam in a later phase.
func (s *Signer) Sign(pdf []byte, signedAt time.Time) Signature {
	digest := sha256.Sum256(pdf)
	hexDigest := hex.EncodeToString(digest[:])
	msg := signedMessage(hexDigest, signedAt)
	sig := ed25519.Sign(s.priv, msg)
	return Signature{
		Algorithm:   "ed25519",
		PublicKeyID: s.publicKeyID,
		Value:       base64.StdEncoding.EncodeToString(sig),
		PDFSHA256:   hexDigest,
		SignedAt:    signedAt,
	}
}

// Verify checks a signature against PDF bytes. It returns nil only when the
// signature is well-formed, the digest matches the supplied PDF, and the
// signature verifies under pub.
func Verify(pub ed25519.PublicKey, pdf []byte, sig Signature) error {
	if sig.Algorithm != "ed25519" {
		return fmt.Errorf("evidence: unsupported algorithm %q", sig.Algorithm)
	}
	digest := sha256.Sum256(pdf)
	hexDigest := hex.EncodeToString(digest[:])
	if hexDigest != sig.PDFSHA256 {
		return errors.New("evidence: PDF digest does not match signature (tampered or wrong file)")
	}
	raw, err := base64.StdEncoding.DecodeString(sig.Value)
	if err != nil {
		return fmt.Errorf("evidence: signature not valid base64: %w", err)
	}
	msg := signedMessage(hexDigest, sig.SignedAt)
	if !ed25519.Verify(pub, msg, raw) {
		return errors.New("evidence: signature does not verify")
	}
	return nil
}

func signedMessage(hexDigest string, signedAt time.Time) []byte {
	return []byte(hexDigest + "|" + signedAt.UTC().Format(time.RFC3339Nano))
}

func keyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])
}
