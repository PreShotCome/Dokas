package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/preshotcome/anything/internal/apikey"
	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/web/templates"
)

func (h *Handlers) apiKeysList(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	keys, _ := h.apiKeys.List(r.Context(), lc.Account.ID)
	render(w, r, templates.APIKeysPage(lc, keys, ""))
}

func (h *Handlers) apiKeyCreate(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	if name == "" {
		name = "API key"
	}
	key, err := h.apiKeys.Create(r.Context(), lc.Account.ID, lc.User.ID, name)
	if err != nil {
		http.Error(w, "create key: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &lc.Account.ID, ActorID: &lc.User.ID, Action: "apikey.created",
		TargetKind: "api_key", TargetID: key.ID.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
		Metadata: map[string]any{"name": name},
	})
	// Render the list with the raw secret shown once — never redirected or
	// logged (a query param would leak it into access logs).
	keys, _ := h.apiKeys.List(r.Context(), lc.Account.ID)
	render(w, r, templates.APIKeysPage(lc, keys, key.Secret))
}

func (h *Handlers) apiKeyRevoke(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	keyID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := h.apiKeys.Revoke(r.Context(), lc.Account.ID, keyID); err != nil {
		if errors.Is(err, apikey.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "revoke: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &lc.Account.ID, ActorID: &lc.User.ID, Action: "apikey.revoked",
		TargetKind: "api_key", TargetID: keyID.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
	})
	http.Redirect(w, r, "/account/api-keys", http.StatusSeeOther)
}
