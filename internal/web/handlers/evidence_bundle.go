package handlers

import (
	"archive/zip"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/preshotcome/dokaz/internal/account"
	"github.com/preshotcome/dokaz/internal/audit"
	"github.com/preshotcome/dokaz/internal/auth"
	"github.com/preshotcome/dokaz/internal/branding"
)

const bundleReadme = `Dokaz — Evidence Bundle
=======================

This archive contains every signed Proof-of-Recovery report your account
produced in the selected window.

  reports/      the signed PDF for each drill
  signatures/   the detached Ed25519 signature for each PDF
  manifest.csv  one row per report: database, date, result, signing key

How to verify a report is genuine and unaltered:
  1. Online — upload the PDF + its signature at https://` + branding.DomainSite + `/verify
  2. Offline — run the open-source dokaz-verify CLI against the published key:
       https://` + branding.DomainSite + `/.well-known/evidence-signing-keys.pem

Each report is independently verifiable: nothing here depends on trusting Dokaz.
`

// evidenceBundle streams a ZIP of every signed report for the account over the
// last 12 months — the artifact an auditor asks for at renewal. Paid plans
// only (free/trial accounts only have sample evidence).
func (h *Handlers) evidenceBundle(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	if !account.IsPaid(lc.Account.Plan) {
		http.Error(w, "The evidence bundle is a paid-plan feature.", http.StatusForbidden)
		return
	}
	since := monthsAgoUTC(time.Now(), 12)
	drills, err := h.drills.ListEvidenceDrills(r.Context(), lc.Account.ID, since)
	if err != nil {
		http.Error(w, "Could not assemble the bundle — please try again.", http.StatusInternalServerError)
		return
	}
	// Scope the bundle to databases this member may see (issue #29): names
	// doubles as the visibility set — a drill whose target is absent is
	// excluded below, so the bundle never leaks another team's evidence.
	targets, _ := h.drills.ListTargets(r.Context(), lc.Account.ID, h.databaseScope(r, lc))
	names := make(map[string]string, len(targets))
	for _, t := range targets {
		names[t.ID.String()] = t.Name
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+branding.Slug+`-evidence-bundle.zip"`)
	zw := zip.NewWriter(w)
	defer zw.Close()

	mf, err := zw.Create("manifest.csv")
	if err != nil {
		return
	}
	cw := csv.NewWriter(mf)
	_ = cw.Write([]string{"drill_id", "database", "completed_at", "result", "retain_until", "signing_key", "pdf_file"})

	included := 0
	for _, d := range drills {
		if d.EvidencePath == nil {
			continue
		}
		pdf, err := h.evidence.Read(r.Context(), lc.Account.ID, *d.EvidencePath)
		if err != nil {
			continue // shredded or unreadable — skip rather than fail the whole bundle
		}
		db, visible := names[d.TargetID.String()]
		if !visible {
			continue // database outside this member's team scope — exclude
		}
		date := ""
		if d.CompletedAt != nil {
			date = d.CompletedAt.UTC().Format("2006-01-02")
		}
		base := fmt.Sprintf("%s-%s-%s", slugify(db), date, d.ID.String()[:8])

		pdfName := "reports/" + base + ".pdf"
		if pw, err := zw.Create(pdfName); err == nil {
			_, _ = pw.Write(pdf)
		}

		signingKey, retainUntil := "", ""
		if sig, err := h.evidence.GetSignature(r.Context(), d.ID); err == nil {
			signingKey = sig.PublicKeyID
			retainUntil = sig.RetainUntil.UTC().Format("2006-01-02")
			sj, _ := json.MarshalIndent(map[string]any{
				"algorithm":     sig.Algorithm,
				"public_key_id": sig.PublicKeyID,
				"value":         sig.Value,
				"pdf_sha256":    sig.PDFSHA256,
				"signed_at":     sig.SignedAt,
				"retain_until":  sig.RetainUntil,
			}, "", "  ")
			if sw, err := zw.Create("signatures/" + base + ".json"); err == nil {
				_, _ = sw.Write(sj)
			}
		}
		_ = cw.Write([]string{d.ID.String(), db, date, string(d.Status), retainUntil, signingKey, pdfName})
		included++
	}
	cw.Flush()

	if rw, err := zw.Create("README.txt"); err == nil {
		_, _ = rw.Write([]byte(bundleReadme))
	}

	if u, ok := auth.FromContext(r.Context()); ok {
		_ = h.audit.Record(r.Context(), audit.Event{
			AccountID: &lc.Account.ID, ActorID: &u.ID, Action: "evidence.bundle_downloaded",
			TargetKind: "account", TargetID: lc.Account.ID.String(),
			Metadata: map[string]any{"reports": included},
			IP:        audit.ClientIP(r), UserAgent: r.UserAgent(),
		})
	}
}

// slugify reduces a database name to a filename-safe token.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "database"
	}
	return out
}
