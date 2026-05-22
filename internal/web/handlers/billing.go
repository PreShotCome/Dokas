package handlers

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/billing"
	"github.com/preshotcome/anything/internal/web/templates"
)

// pricingPage renders the public pricing page. It is reachable signed out
// (the call to action sends visitors to signup) and signed in (an account
// owner can subscribe straight from here).
func (h *Handlers) pricingPage(w http.ResponseWriter, r *http.Request) {
	render(w, r, templates.Pricing(
		h.layoutCtx(r), h.priceStarterLabel, h.priceProLabel, h.billing.Enabled()))
}

// billingCheckout starts a Stripe Checkout Session for a plan and redirects
// the account owner to Stripe's hosted payment page.
func (h *Handlers) billingCheckout(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	plan := strings.TrimSpace(r.PostFormValue("plan"))
	priceID := h.billing.PriceID(plan)
	if priceID == "" {
		http.Error(w, "unknown or unconfigured plan", http.StatusBadRequest)
		return
	}

	customerID, err := h.ensureStripeCustomer(r.Context(), lc)
	if err != nil {
		http.Error(w, "billing: "+err.Error(), http.StatusInternalServerError)
		return
	}
	url, err := h.billing.Checkout(r.Context(), billing.CheckoutInput{
		CustomerID: customerID,
		PriceID:    priceID,
		SuccessURL: absoluteURL(r, "/account?billing=success"),
		CancelURL:  absoluteURL(r, "/account"),
	})
	if err != nil {
		http.Error(w, "billing: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &lc.Account.ID, ActorID: &lc.User.ID, Action: "billing.checkout_started",
		TargetKind: "account", TargetID: lc.Account.ID.String(),
		Metadata: map[string]any{"plan": plan},
	})
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// billingPortal opens the Stripe Customer Portal where the account owner can
// change or cancel the subscription and download invoices.
func (h *Handlers) billingPortal(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	if lc.Account.StripeCustomerID == nil {
		http.Error(w, "no billing account yet — subscribe to a plan first", http.StatusBadRequest)
		return
	}
	url, err := h.billing.Portal(r.Context(), *lc.Account.StripeCustomerID, absoluteURL(r, "/account"))
	if err != nil {
		http.Error(w, "billing: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// ensureStripeCustomer returns the account's Stripe customer ID, creating one
// on demand if the account doesn't have one yet.
func (h *Handlers) ensureStripeCustomer(ctx context.Context, lc templates.LayoutCtx) (string, error) {
	if lc.Account.StripeCustomerID != nil && *lc.Account.StripeCustomerID != "" {
		return *lc.Account.StripeCustomerID, nil
	}
	id, err := h.billing.Create(ctx, lc.Account.ID, lc.User.Email, lc.Account.Name)
	if err != nil {
		return "", err
	}
	if err := h.accounts.SetStripeCustomerID(ctx, lc.Account.ID, id); err != nil {
		return "", err
	}
	return id, nil
}

// stripeWebhook receives Stripe events. The Stripe-Signature header is the
// only authentication, so an unverified body is rejected before any work.
func (h *Handlers) stripeWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "could not read body", http.StatusBadRequest)
		return
	}
	if err := h.billing.VerifyWebhook(body, r.Header.Get("Stripe-Signature")); err != nil {
		http.Error(w, "invalid signature", http.StatusBadRequest)
		return
	}
	ev, err := billing.ParseWebhook(body)
	if err != nil {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}
	if !billing.IsSubscriptionEvent(ev.Type) {
		w.WriteHeader(http.StatusOK) // acknowledged, nothing to do
		return
	}

	// Map the event to a plan tier. A deleted subscription drops the account
	// back to the trial tier.
	plan, subID, status := billing.PlanTrial, ev.SubscriptionID, ev.Status
	if ev.Type == "customer.subscription.deleted" {
		subID, status = "", "canceled"
	} else {
		plan = h.billing.PlanForPrice(ev.PriceID)
	}
	if err := h.accounts.SyncSubscription(r.Context(), ev.CustomerID, subID, status, plan); err != nil {
		// 5xx so Stripe retries the delivery.
		http.Error(w, "sync failed", http.StatusInternalServerError)
		return
	}
	h.logger().Info("stripe subscription synced",
		"event", ev.Type, "customer", ev.CustomerID, "plan", plan, "status", status)
	w.WriteHeader(http.StatusOK)
}
