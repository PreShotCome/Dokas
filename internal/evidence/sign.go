// Package evidence handles tamper-evidence for drill reports: detached
// signatures, an evidence store abstraction, and retention metadata.
//
// Production deployments inject a real document-signing key (and, later, a
// cert chain + RFC 3161 timestamp authority). Locally the signer falls back
// to an ephemeral key so dev and CI work without secrets — the signing and
// verification machinery is identical either way.
package evidence

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"sort"
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

// Signer produces detached signatures and verifies them. It signs with one
// active key but can verify against a set: the active key plus any retired
// keys, so evidence signed before a key rotation still verifies. Safe for
// concurrent use (verifyKeys is read-only after construction).
type Signer struct {
	priv        ed25519.PrivateKey
	publicKeyID string
	ephemeral   bool
	// verifyKeys maps a public-key fingerprint to its key. Always contains
	// the active key; retired keys are added from EVIDENCE_VERIFICATION_KEYS.
	verifyKeys map[string]ed25519.PublicKey
}

// NewSigner builds a Signer with only its active key. Equivalent to
// NewSignerWithVerificationKeys(keyPEM, "").
func NewSigner(keyPEM string) (*Signer, error) {
	return NewSignerWithVerificationKeys(keyPEM, "")
}

// NewSignerWithVerificationKeys builds a Signer. keyPEM is a PKCS#8 Ed25519
// private key in PEM form (the EVIDENCE_SIGNING_KEY config value); when empty
// an ephemeral key is generated — fine for dev/CI, never for production.
//
// verifyKeysPEM is zero or more concatenated PEM public-key blocks (the
// EVIDENCE_VERIFICATION_KEYS config value): the public halves of keys retired
// by rotation. Their signatures still verify even though they no longer sign.
func NewSignerWithVerificationKeys(keyPEM, verifyKeysPEM string) (*Signer, error) {
	var (
		priv      ed25519.PrivateKey
		ephemeral bool
	)
	if keyPEM == "" {
		_, generated, err := ed25519.GenerateKey(nil)
		if err != nil {
			return nil, fmt.Errorf("generate ephemeral key: %w", err)
		}
		priv, ephemeral = generated, true
	} else {
		block, _ := pem.Decode([]byte(keyPEM))
		if block == nil {
			return nil, errors.New("evidence: EVIDENCE_SIGNING_KEY is not valid PEM")
		}
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse signing key: %w", err)
		}
		var ok bool
		priv, ok = parsed.(ed25519.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("evidence: signing key is %T, want ed25519", parsed)
		}
	}

	pub := priv.Public().(ed25519.PublicKey)
	s := &Signer{
		priv:        priv,
		publicKeyID: keyID(pub),
		ephemeral:   ephemeral,
		verifyKeys:  map[string]ed25519.PublicKey{keyID(pub): pub},
	}

	rest := []byte(verifyKeysPEM)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("evidence: EVIDENCE_VERIFICATION_KEYS: %w", err)
		}
		retired, ok := parsed.(ed25519.PublicKey)
		if !ok {
			return nil, fmt.Errorf("evidence: verification key is %T, want ed25519", parsed)
		}
		s.verifyKeys[keyID(retired)] = retired
	}
	return s, nil
}

// VerificationKey returns the public key registered under keyID — the active
// key or a retired verification key — and whether one was found.
func (s *Signer) VerificationKey(keyID string) (ed25519.PublicKey, bool) {
	pub, ok := s.verifyKeys[keyID]
	return pub, ok
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

// AllPublicKeysPEM serializes every key this Signer will verify against —
// the active signing key plus any retired verification keys — as a single
// PEM document. Each block is preceded by two comment lines naming its
// fingerprint and status:
//
//	# PublicKeyID: <hex fingerprint>
//	# Status: active
//	-----BEGIN PUBLIC KEY-----
//	...
//	-----END PUBLIC KEY-----
//
// The comments sit OUTSIDE the PEM block on purpose: OpenSSL (and Go's own
// pem.Decode) treat any non-PEM line between blocks as ignorable preamble,
// but header lines inside a block break parsing. So this document round-trips
// through `openssl pkey -pubin` while still being self-describing to a human.
//
// The active key is emitted first; retired keys follow in fingerprint order
// so the output is stable across calls.
func (s *Signer) AllPublicKeysPEM() (string, error) {
	ids := make([]string, 0, len(s.verifyKeys))
	for id := range s.verifyKeys {
		if id != s.publicKeyID {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	// Active key leads; retired keys follow in deterministic order.
	ordered := append([]string{s.publicKeyID}, ids...)

	var buf bytes.Buffer
	for _, id := range ordered {
		pub := s.verifyKeys[id]
		der, err := x509.MarshalPKIXPublicKey(pub)
		if err != nil {
			return "", fmt.Errorf("evidence: marshal public key %s: %w", id, err)
		}
		status := "retired"
		if id == s.publicKeyID {
			status = "active"
		}
		fmt.Fprintf(&buf, "# PublicKeyID: %s\n# Status: %s\n", id, status)
		if err := pem.Encode(&buf, &pem.Block{Type: "PUBLIC KEY", Bytes: der}); err != nil {
			return "", fmt.Errorf("evidence: encode public key %s: %w", id, err)
		}
	}
	return buf.String(), nil
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
