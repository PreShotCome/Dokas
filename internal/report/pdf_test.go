// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package report

import (
	"bytes"
	"compress/zlib"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/preshotcome/dokaz/internal/drill"
)

// inflate mirrors the e2e-smoke PDF text extractor: it returns the raw bytes
// plus every deflate-decoded stream, so a substring search finds text whether
// or not fpdf compressed it.
func inflate(pdf []byte) []byte {
	var out bytes.Buffer
	out.Write(pdf)
	out.WriteByte('\n')
	for i := 0; i < len(pdf); {
		s := bytes.Index(pdf[i:], []byte("stream\n"))
		if s < 0 {
			break
		}
		s += i + len("stream\n")
		e := bytes.Index(pdf[s:], []byte("\nendstream"))
		if e < 0 {
			break
		}
		if r, err := zlib.NewReader(bytes.NewReader(pdf[s : s+e])); err == nil {
			if b, err := io.ReadAll(r); err == nil {
				out.Write(b)
				out.WriteByte('\n')
			}
			r.Close()
		}
		i = s + e + len("\nendstream")
	}
	return out.Bytes()
}

func sampleData(operator string) Data {
	now := time.Now().UTC()
	started := now.Add(-90 * time.Second)
	return Data{
		Drill: drill.Drill{
			ID:          uuid.New(),
			Status:      drill.StatusSucceeded,
			StartedAt:   &started,
			CompletedAt: &now,
		},
		Target: drill.Target{
			Name:       "Prod Postgres",
			SourceKind: "postgres_dump_local",
			SourceURI:  "prod.dump",
		},
		// One passing assertion so the verdict reads as an auditor-grade
		// PASSED. A restore-only drill (zero assertions) now correctly
		// renders as "RESTORED (no assertions configured)".
		Assertions: []drill.AssertionResult{{
			Kind:     "table_exists",
			Expected: []byte("events"),
			Actual:   []byte("present"),
			Passed:   true,
		}},
		Operator:    operator,
		GeneratedAt: now,
	}
}

func TestRenderProofOfRecoveryContent(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleData("ops@example.com")); err != nil {
		t.Fatalf("render: %v", err)
	}
	text := inflate(buf.Bytes())

	// The verdict string the e2e-smoke (and every auditor) keys on.
	mustContain(t, text, "Verdict: PASSED")
	// The reframed, auditor-recognisable title + compliance citations.
	for _, want := range []string{
		"Proof of Recovery",
		"ISO/IEC 27001:2022 Annex A 8.13",
		"A1.3",
		"cyber-insurance",
		"Operator",
		"ops@example.com",
	} {
		mustContain(t, text, want)
	}
}

func TestRenderOmitsOperatorWhenEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleData("")); err != nil {
		t.Fatalf("render: %v", err)
	}
	if bytes.Contains(inflate(buf.Bytes()), []byte("Operator")) {
		t.Error("Operator row should be omitted when Operator is empty")
	}
}

// TestRenderIsMojibakeFree guards the Latin-1 font seam: the PDF body must not
// carry the UTF-8-read-as-Latin-1 byte sequence that a stray em-dash or smart
// quote in a literal would produce (the bug the e2e-smoke also checks for).
func TestRenderIsMojibakeFree(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleData("ops@example.com")); err != nil {
		t.Fatalf("render: %v", err)
	}
	if bytes.Contains(buf.Bytes(), []byte{0xc3, 0xa2, 0xe2, 0x82, 0xac}) {
		t.Error("PDF contains mojibake (UTF-8 char in a Latin-1 literal) - use ASCII in pdf.go")
	}
}

func mustContain(t *testing.T, haystack []byte, needle string) {
	t.Helper()
	if !bytes.Contains(haystack, []byte(needle)) {
		t.Errorf("rendered PDF missing %q", needle)
	}
}

// TestZeroAssertionVerdict guards against auditor-grade "PASSED" reports for
// drills that ran zero assertions. A restore-only drill is real evidence the
// dump can be opened, but not that the data survived — the report must say so
// plainly rather than stamping "PASSED".
func TestZeroAssertionVerdict(t *testing.T) {
	data := sampleData("ops@example.com")
	data.Assertions = nil // no assertions configured

	var buf bytes.Buffer
	if err := Render(&buf, data); err != nil {
		t.Fatalf("render: %v", err)
	}
	text := inflate(buf.Bytes())

	if bytes.Contains(text, []byte("Verdict: PASSED")) {
		t.Errorf("zero-assertion drill must NOT stamp 'Verdict: PASSED'")
	}
	// FPDF escapes '(' and ')' in text-stream operators (they are PDF
	// string delimiters), so we search for the unparenthesised substrings.
	mustContain(t, text, "Verdict: RESTORED")
	mustContain(t, text, "no assertions configured")
}
