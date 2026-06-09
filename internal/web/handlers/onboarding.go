package handlers

import (
	_ "embed"
	"net/http"
)

// sampleDump is a tiny PostgreSQL custom-format dump (one `public.events`
// table) embedded so a new user can download a known-good backup and run
// their first drill against it without finding their own database. It's a
// copy of testdata/fixtures/tiny.dump — the same artifact the e2e-smoke
// harness exercises, so the onboarding path and the CI path stay aligned.
//
//go:embed sample.dump
var sampleDump []byte

// onboardingSampleDump serves the embedded fixture as a file download. It's
// public — the whole point is to hand a frictionless first artifact to
// someone who hasn't connected a real source yet. Cached for a day; the
// fixture only changes when we redeploy.
func (h *Handlers) onboardingSampleDump(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="sample.dump"`)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(sampleDump)
}
