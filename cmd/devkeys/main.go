// Command devkeys generates stable EVIDENCE_ENCRYPTION_KEY and
// EVIDENCE_SIGNING_KEY values for local development, then prints them
// as KEY=value lines on stdout so dev.ps1 / make dev can save them to
// tmp/dev-keys.env and source them on every subsequent run.
//
// Without persistent keys, the server falls back to ephemeral ones that
// reset every restart, which means:
//   - Anything encrypted in a prior run (e.g. per-account wrap keys)
//     fails to unwrap on the next run with "message authentication failed".
//   - Signatures over previously-emitted PDFs no longer verify.
//
// NOT FOR PRODUCTION — these keys go in a plaintext file. Use a real
// secret manager (Fly secrets, KMS, Vault) for prod.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"log"
)

func main() {
	// 32 random bytes, base64-encoded — the AES-256 master key.
	enc := make([]byte, 32)
	if _, err := rand.Read(enc); err != nil {
		log.Fatalf("read random: %v", err)
	}
	encB64 := base64.StdEncoding.EncodeToString(enc)

	// Ed25519 private key, PKCS#8-encoded, PEM-wrapped.
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatalf("generate ed25519: %v", err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		log.Fatalf("marshal pkcs8: %v", err)
	}
	signPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})

	// Print as a sourceable env file. The signing key is multi-line PEM, so
	// emit it on one base64-encoded line and let dev.ps1 decode it back.
	signB64 := base64.StdEncoding.EncodeToString(signPEM)
	fmt.Printf("EVIDENCE_ENCRYPTION_KEY=%s\n", encB64)
	fmt.Printf("EVIDENCE_SIGNING_KEY_B64=%s\n", signB64)
}
