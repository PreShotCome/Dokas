// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package handlers

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/preshotcome/dokaz/internal/evidence"
)

// postVerify builds a multipart POST to /verify with the given pdf + signature
// JSON and runs it through the handler, returning the rendered HTML body.
func postVerify(t *testing.T, h *Handlers, pdf, sigJSON []byte) string {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	pw, _ := mw.CreateFormFile("pdf", "evidence.pdf")
	pw.Write(pdf)
	sw, _ := mw.CreateFormFile("signature", "signature.json")
	sw.Write(sigJSON)
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/verify", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	h.verifyEvidence(rr, req)
	return rr.Body.String()
}

func TestVerifyEvidence(t *testing.T) {
	signer, err := evidence.NewSigner("") // ephemeral ed25519 key
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	h := &Handlers{signer: signer}

	pdf := []byte("%PDF-1.4\nProof-of-Recovery: PASSED\n%%EOF")
	sig := signer.Sign(pdf, time.Now().UTC())
	sigJSON, _ := json.Marshal(map[string]any{"data": map[string]any{
		"algorithm":     sig.Algorithm,
		"public_key_id": sig.PublicKeyID,
		"value":         sig.Value,
		"pdf_sha256":    sig.PDFSHA256,
		"signed_at":     sig.SignedAt,
	}})

	// Happy path: genuine PDF + signature verifies.
	// The verified surface renders a "Verification receipt" panel with a
	// lowercase "verified" status pill; the body must NOT contain the
	// "Not verified" negative marker.
	if body := postVerify(t, h, pdf, sigJSON); !strings.Contains(body, "Verification receipt") || strings.Contains(body, "Not verified") {
		t.Errorf("genuine report should verify; body did not show Verification receipt")
	}

	// Tampered PDF: one byte changed must fail the digest check.
	tampered := append([]byte{}, pdf...)
	tampered[10] ^= 0xFF
	if body := postVerify(t, h, tampered, sigJSON); !strings.Contains(body, "Not verified") {
		t.Errorf("tampered PDF should NOT verify")
	}

	// Unknown key: a signature claiming a different fingerprint is rejected.
	badKeyJSON, _ := json.Marshal(map[string]any{"data": map[string]any{
		"algorithm": "ed25519", "public_key_id": "deadbeef", "value": sig.Value,
		"pdf_sha256": sig.PDFSHA256, "signed_at": sig.SignedAt,
	}})
	if body := postVerify(t, h, pdf, badKeyJSON); !strings.Contains(body, "Unknown signing-key") {
		t.Errorf("unknown key should be rejected with a clear message")
	}
}
