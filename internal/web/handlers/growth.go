package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// robotsTxt disallows indexing the application. The app is private; the
// marketing site (separate repo) is what search engines should crawl.
func (h *Handlers) robotsTxt(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
}

// postmarkBounce is Postmark's inbound bounce/complaint webhook. The {token}
// path segment must match POSTMARK_WEBHOOK_TOKEN — Postmark webhooks have no
// other authentication. When the token is unconfigured the route 404s.
func (h *Handlers) postmarkBounce(w http.ResponseWriter, r *http.Request) {
	if h.postmarkWebhookToken == "" {
		http.NotFound(w, r)
		return
	}
	token := chi.URLParam(r, "token")
	if subtle.ConstantTimeCompare([]byte(token), []byte(h.postmarkWebhookToken)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Postmark bounce and spam-complaint payloads both carry Email; the
	// reason comes from Type (bounce) or RecordType (complaint).
	var payload struct {
		RecordType  string `json:"RecordType"`
		Type        string `json:"Type"`
		Email       string `json:"Email"`
		Description string `json:"Description"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&payload); err != nil {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(strings.ToLower(payload.Email))
	if email == "" {
		http.Error(w, "missing Email", http.StatusBadRequest)
		return
	}

	reason := payload.Type
	if reason == "" {
		reason = payload.RecordType
	}
	if reason == "" {
		reason = "unknown"
	}
	if err := h.mailer.Suppress(r.Context(), email, reason, payload.Description); err != nil {
		http.Error(w, "suppress: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.logger().Info("email address suppressed via postmark webhook",
		"email", email, "reason", reason)
	w.WriteHeader(http.StatusOK)
}
