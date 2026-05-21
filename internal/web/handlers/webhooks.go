package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/web/templates"
	"github.com/preshotcome/anything/internal/webhooks"
)

func (h *Handlers) webhooksList(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	endpoints, _ := h.webhooks.ListEndpoints(r.Context(), lc.Account.ID)
	// A freshly-created endpoint's secret is surfaced once via ?new=<id>.
	revealID := r.URL.Query().Get("new")
	render(w, r, templates.WebhooksPage(lc, endpoints, revealID))
}

func (h *Handlers) webhookCreate(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	rawURL := strings.TrimSpace(r.PostFormValue("url"))
	if !validWebhookURL(rawURL) {
		endpoints, _ := h.webhooks.ListEndpoints(r.Context(), lc.Account.ID)
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, templates.WebhooksPageWithError(lc, endpoints,
			"Enter a valid http(s) URL."))
		return
	}

	e, err := h.webhooks.CreateEndpoint(r.Context(), lc.Account.ID, rawURL)
	if err != nil {
		http.Error(w, "create webhook: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &lc.Account.ID, ActorID: &lc.User.ID, Action: "webhook.created",
		TargetKind: "webhook_endpoint", TargetID: e.ID.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
		Metadata: map[string]any{"url": rawURL},
	})
	// Redirect with ?new= so the page can reveal the secret one time.
	http.Redirect(w, r, "/account/webhooks?new="+e.ID.String(), http.StatusSeeOther)
}

func (h *Handlers) webhookDelete(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	endpointID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := h.webhooks.DeleteEndpoint(r.Context(), lc.Account.ID, endpointID); err != nil {
		if errors.Is(err, webhooks.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "delete: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &lc.Account.ID, ActorID: &lc.User.ID, Action: "webhook.deleted",
		TargetKind: "webhook_endpoint", TargetID: endpointID.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
	})
	http.Redirect(w, r, "/account/webhooks", http.StatusSeeOther)
}

func (h *Handlers) webhookDeliveries(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	endpointID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	endpoint, err := h.webhooks.GetEndpoint(r.Context(), lc.Account.ID, endpointID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	deliveries, _ := h.webhooks.ListDeliveries(r.Context(), lc.Account.ID, endpointID, 100)
	render(w, r, templates.WebhookDeliveries(lc, endpoint, deliveries))
}

// webhookReplay re-sends a past delivery. It creates a fresh delivery row
// (same endpoint, event, and payload) and enqueues that — the original row
// stays an immutable record, and the replay shows as its own timeline entry.
func (h *Handlers) webhookReplay(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	endpointID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	deliveryID, err := uuid.Parse(chi.URLParam(r, "delivery_id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Authorize: the delivery must belong to this account + endpoint.
	d, err := h.webhooks.GetDelivery(r.Context(), deliveryID)
	if err != nil || d.AccountID != lc.Account.ID || d.EndpointID != endpointID {
		http.NotFound(w, r)
		return
	}

	newID, err := h.webhooks.CreateDelivery(r.Context(), d.EndpointID, d.AccountID, d.Event, d.Payload)
	if err != nil {
		http.Error(w, "replay: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.webhookDispatch.Enqueue(r.Context(), newID); err != nil {
		http.Error(w, "replay: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &lc.Account.ID, ActorID: &lc.User.ID, Action: "webhook.replayed",
		TargetKind: "webhook_delivery", TargetID: deliveryID.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
		Metadata: map[string]any{"replay_delivery_id": newID.String()},
	})
	http.Redirect(w, r, "/account/webhooks/"+endpointID.String()+"/deliveries", http.StatusSeeOther)
}

// validWebhookURL accepts only absolute http(s) URLs with a host. It does
// not block private IPs — SSRF hardening of the delivery worker is a
// later-phase concern noted in the runbook.
func validWebhookURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Host != ""
}
