// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/preshotcome/dokaz/internal/evidence"
	"github.com/preshotcome/dokaz/internal/web/templates"
)

// maxVerifyUpload caps each uploaded file. Evidence PDFs are a few KB and the
// signature JSON is tiny; 16 MB is generous headroom and bounds the work a
// public, unauthenticated endpoint will do.
const maxVerifyUpload = 16 << 20

// verifyEvidencePage renders the public "verify a report" tool — an auditor
// who was handed a Dokaz PDF + signature can confirm here that it is genuine
// and unaltered, without a Dokaz account.
func (h *Handlers) verifyEvidencePage(w http.ResponseWriter, r *http.Request) {
	render(w, r, templates.VerifyEvidence(h.layoutCtx(r), nil))
}

// verifyEvidence checks an uploaded PDF against its detached signature JSON,
// using the same public keys Dokaz publishes at the well-known endpoint. It
// returns a pass/fail verdict — never stores the upload.
func (h *Handlers) verifyEvidence(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	fail := func(msg string) {
		render(w, r, templates.VerifyEvidence(lc, &templates.VerifyResult{OK: false, Message: msg}))
	}

	if err := r.ParseMultipartForm(maxVerifyUpload); err != nil {
		fail("Could not read the upload. Make sure both files are attached and under 16 MB.")
		return
	}
	pdf, err := readUpload(r, "pdf")
	if err != nil {
		fail("Attach the evidence PDF (the file downloaded from the drill's Evidence link).")
		return
	}
	sigRaw, err := readUpload(r, "signature")
	if err != nil {
		fail("Attach the signature JSON (the file from the drill's Signature link).")
		return
	}

	sig, err := parseSignatureJSON(sigRaw)
	if err != nil {
		fail("The signature file is not valid Dokaz signature JSON.")
		return
	}

	pub, ok := h.signer.VerificationKey(sig.PublicKeyID)
	if !ok {
		fail("Unknown signing-key fingerprint " + sig.PublicKeyID + " — this report was not signed by this Dokaz instance.")
		return
	}
	if err := evidence.Verify(pub, pdf, sig); err != nil {
		fail("Verification FAILED — " + err.Error())
		return
	}

	render(w, r, templates.VerifyEvidence(lc, &templates.VerifyResult{
		OK:        true,
		Message:   "This report is authentic and unaltered.",
		KeyID:     sig.PublicKeyID,
		SignedAt:  sig.SignedAt.UTC().Format("2006-01-02 15:04 MST"),
		PDFSHA256: sig.PDFSHA256,
	}))
}

// readUpload reads a named multipart file fully, bounded by maxVerifyUpload.
func readUpload(r *http.Request, field string) ([]byte, error) {
	f, _, err := r.FormFile(field)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxVerifyUpload))
}

// parseSignatureJSON accepts either the API envelope ({"data": {...}}) or a
// bare signature object, and maps it to an evidence.Signature.
func parseSignatureJSON(raw []byte) (evidence.Signature, error) {
	type sigJSON struct {
		Algorithm   string    `json:"algorithm"`
		PublicKeyID string    `json:"public_key_id"`
		Value       string    `json:"value"`
		PDFSHA256   string    `json:"pdf_sha256"`
		SignedAt    time.Time `json:"signed_at"`
	}
	var env struct {
		Data *sigJSON `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err == nil && env.Data != nil {
		s := env.Data
		return evidence.Signature{Algorithm: s.Algorithm, PublicKeyID: s.PublicKeyID, Value: s.Value, PDFSHA256: s.PDFSHA256, SignedAt: s.SignedAt}, nil
	}
	var bare sigJSON
	if err := json.Unmarshal(raw, &bare); err != nil {
		return evidence.Signature{}, err
	}
	return evidence.Signature{Algorithm: bare.Algorithm, PublicKeyID: bare.PublicKeyID, Value: bare.Value, PDFSHA256: bare.PDFSHA256, SignedAt: bare.SignedAt}, nil
}
