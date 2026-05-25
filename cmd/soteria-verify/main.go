// Command soteria-verify independently checks a Soteria evidence PDF
// against its detached Ed25519 signature and a public key. It depends on
// the Go standard library only (crypto/ed25519, crypto/sha256, encoding/
// pem, encoding/json) — no Soteria-specific code path — so any auditor
// can build it from this source and audit what they're trusting.
//
// Usage:
//
//	soteria-verify --pdf=evidence.pdf --sig=signature.json --pubkey=soteria.pem
//
//	  --pdf      path to the evidence PDF downloaded from /v1/drills/{id}/evidence
//	  --sig      path to the signature JSON downloaded from /v1/drills/{id}/signature
//	  --pubkey   path to a PEM-encoded Ed25519 public key (Soteria publishes the
//	             active and any retired keys at https://soteria.io/.well-known/
//	             evidence-signing-keys.pem — verify the key fingerprint in the
//	             signature matches one published there)
//
// Exit codes:
//
//	0   signature is valid
//	1   signature is invalid (algorithm, digest, or Ed25519 check fails)
//	2   bad input (file missing, malformed JSON, wrong key type, etc.)
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"
)

// signature is the wire shape returned by GET /v1/drills/{id}/signature.
// The field tags match the API exactly; future fields may be added but
// existing ones are stable.
type signature struct {
	Algorithm   string    `json:"algorithm"`
	PublicKeyID string    `json:"public_key_id"`
	Value       string    `json:"value"`
	PDFSHA256   string    `json:"pdf_sha256"`
	SignedAt    time.Time `json:"signed_at"`
	RetainUntil time.Time `json:"retain_until"`
}

// envelope mirrors the Soteria API response wrapper { data: ..., meta: ..., errors: ... }
// so the CLI accepts either the bare signature JSON or the wrapped form.
type envelope struct {
	Data *signature `json:"data"`
}

func main() {
	pdfPath := flag.String("pdf", "", "path to the evidence PDF")
	sigPath := flag.String("sig", "", "path to the signature JSON")
	pubPath := flag.String("pubkey", "", "path to the PEM-encoded Ed25519 public key")
	flag.Parse()

	if *pdfPath == "" || *sigPath == "" || *pubPath == "" {
		fmt.Fprintln(os.Stderr, "usage: soteria-verify --pdf=PDF --sig=JSON --pubkey=PEM")
		os.Exit(2)
	}

	pdf, err := os.ReadFile(*pdfPath)
	if err != nil {
		fatal(2, "read pdf: %v", err)
	}
	sigRaw, err := os.ReadFile(*sigPath)
	if err != nil {
		fatal(2, "read signature: %v", err)
	}
	pubPEM, err := os.ReadFile(*pubPath)
	if err != nil {
		fatal(2, "read pubkey: %v", err)
	}

	sig, err := parseSignature(sigRaw)
	if err != nil {
		fatal(2, "parse signature: %v", err)
	}
	pub, err := parsePublicKey(pubPEM)
	if err != nil {
		fatal(2, "parse pubkey: %v", err)
	}

	// Cross-check: the supplied pubkey must match the fingerprint the
	// signature claims. Otherwise the verifier might quietly accept a
	// signature from a key that has nothing to do with the supplied PEM.
	fingerprint := keyID(pub)
	if fingerprint != sig.PublicKeyID {
		fatal(1, "FAIL: supplied public key fingerprint %s does not match signature's %s",
			fingerprint, sig.PublicKeyID)
	}

	if err := verify(pub, pdf, sig); err != nil {
		fatal(1, "FAIL: %v", err)
	}
	fmt.Printf("OK  key=%s  signed_at=%s  retain_until=%s\n",
		sig.PublicKeyID, sig.SignedAt.UTC().Format(time.RFC3339), sig.RetainUntil.UTC().Format(time.RFC3339))
}

func parseSignature(raw []byte) (signature, error) {
	// Accept either the wrapped envelope or a bare signature object.
	var env envelope
	if err := json.Unmarshal(raw, &env); err == nil && env.Data != nil {
		return *env.Data, nil
	}
	var bare signature
	if err := json.Unmarshal(raw, &bare); err != nil {
		return signature{}, err
	}
	return bare, nil
}

func parsePublicKey(pemBytes []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("pubkey is not valid PEM")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("pubkey is %T, want ed25519.PublicKey", parsed)
	}
	return pub, nil
}

// verify mirrors evidence.Verify in the main repo exactly. Reimplemented
// here (rather than imported) so the CLI is small, has no transitive
// dependencies on Soteria packages, and can be audited in isolation.
func verify(pub ed25519.PublicKey, pdf []byte, sig signature) error {
	if sig.Algorithm != "ed25519" {
		return fmt.Errorf("unsupported algorithm %q", sig.Algorithm)
	}
	digest := sha256.Sum256(pdf)
	hexDigest := hex.EncodeToString(digest[:])
	if hexDigest != sig.PDFSHA256 {
		return fmt.Errorf("PDF digest does not match signature (got %s, want %s — tampered or wrong file)",
			hexDigest, sig.PDFSHA256)
	}
	raw, err := base64.StdEncoding.DecodeString(sig.Value)
	if err != nil {
		return fmt.Errorf("signature is not valid base64: %w", err)
	}
	// The signed message is: hex(sha256(pdf)) ‖ "|" ‖ signedAt(RFC3339Nano UTC).
	// This is identical to internal/evidence/sign.go.signedMessage.
	msg := []byte(hexDigest + "|" + sig.SignedAt.UTC().Format(time.RFC3339Nano))
	if !ed25519.Verify(pub, msg, raw) {
		return errors.New("Ed25519 signature does not verify under the supplied key")
	}
	return nil
}

func keyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])
}

func fatal(code int, format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(code)
}
