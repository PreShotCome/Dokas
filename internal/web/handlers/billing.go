package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/preshotcome/dokaz/internal/account"
	"github.com/preshotcome/dokaz/internal/audit"
	"github.com/preshotcome/dokaz/internal/billing"
	"github.com/preshotcome/dokaz/internal/branding"
	mail "github.com/preshotcome/dokaz/internal/email"
	"github.com/preshotcome/dokaz/internal/web/templates"
)

// billingFailed logs a billing/Stripe failure with account context and shows
// the user a generic message. Stripe's raw error text can name internal price
// and customer IDs and portal-configuration URLs, so it is logged, never shown
// — and without this log line a prod failure (e.g. an unconfigured live
// Customer Portal) leaves no server-side trace at all.
func (h *Handlers) billingFailed(w http.ResponseWriter, op, accountID string, err error) {
	h.logger().Error("billing operation failed", "op", op, "account_id", accountID, "err", err)
	http.Error(w,
		"Billing is temporarily unavailable. Please try again in a moment; if it keeps happening, email "+branding.SupportEmail+".",
		http.StatusInternalServerError)
}

// postCancellationGrace is how long an account keeps full access after the
// Stripe subscription is canceled. Long enough for the owner to notice and
// resubscribe, short enough that an unrenewed account doesn't drift forever.
const postCancellationGrace = 7 * 24 * time.Hour

// pricingPage renders the public pricing page. It is reachable signed out
// (the call to action sends visitors to signup) and signed in (an account
// owner can subscribe straight from here).
func (h *Handlers) pricingPage(w http.ResponseWriter, r *http.Request) {
	render(w, r, templates.Pricing(
		h.layoutCtx(r), h.priceStarterLabel, h.priceProLabel, h.priceScaleLabel,
		h.billing.Enabled()))
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

	// Already on a paid plan? Open the Customer Portal where they can change
	// plans, instead of creating a second parallel subscription that Stripe
	// would happily bill them for. A canceled account is back on PlanTrial
	// (HandleSubscriptionCanceled clears the sub) so it's free to start a
	// new Checkout from here.
	if lc.Account.Plan != account.PlanTrial {
		if lc.Account.StripeCustomerID == nil || *lc.Account.StripeCustomerID == "" {
			h.logger().Error("billing: paid account has no stripe customer id",
				"op", "checkout:sync_check", "account_id", lc.Account.ID.String())
			http.Error(w, "Your billing account is out of sync — please email "+branding.SupportEmail+".",
				http.StatusInternalServerError)
			return
		}
		portalURL, err := h.billing.Portal(r.Context(), *lc.Account.StripeCustomerID, h.absoluteURL(r, "/account"))
		if err != nil {
			h.billingFailed(w, "checkout:portal_redirect", lc.Account.ID.String(), err)
			return
		}
		http.Redirect(w, r, portalURL, http.StatusSeeOther)
		return
	}

	customerID, err := h.ensureStripeCustomer(r.Context(), lc)
	if err != nil {
		h.billingFailed(w, "checkout:ensure_customer", lc.Account.ID.String(), err)
		return
	}
	url, err := h.billing.Checkout(r.Context(), billing.CheckoutInput{
		CustomerID: customerID,
		PriceID:    priceID,
		SuccessURL: h.absoluteURL(r, "/account?billing=success"),
		CancelURL:  h.absoluteURL(r, "/account"),
	})
	if err != nil {
		h.billingFailed(w, "checkout:create_session", lc.Account.ID.String(), err)
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
	url, err := h.billing.Portal(r.Context(), *lc.Account.StripeCustomerID, h.absoluteURL(r, "/account"))
	if err != nil {
		h.billingFailed(w, "portal:create_session", lc.Account.ID.String(), err)
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// sendPaymentFailedEmail sends the dunning notice to the account owner
// (resolved from the Stripe customer ID) when Stripe reports
// invoice.payment_failed. Best-effort — a mail failure never blocks the
// webhook 200; Stripe would retry it and we'd bounce every dunning attempt
// forever.
func (h *Handlers) sendPaymentFailedEmail(ctx context.Context, customerID string) {
	if customerID == "" {
		return
	}
	owner, err := h.accounts.OwnerByStripeCustomer(ctx, customerID)
	if err != nil {
		h.logger().Warn("dunning: lookup owner failed", "customer", customerID, "err", err)
		return
	}
	// Portal link is the actionable "update card" surface — best-effort; if
	// Stripe rejects the portal session we fall back to /account which links
	// to it too.
	portalLink, err := h.billing.Portal(ctx, customerID, h.baseURL+"/account")
	if err != nil || portalLink == "" {
		portalLink = h.baseURL + "/account"
	}
	if err := h.mailer.Send(ctx, mail.PaymentFailedMessage(owner.OwnerEmail, owner.AccountName, portalLink)); err != nil &&
		!errors.Is(err, mail.ErrSuppressed) {
		h.logger().Warn("dunning email failed", "to", owner.OwnerEmail, "err", err)
	}
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
	// invoice.payment_failed doesn't change plan state — that's
	// customer.subscription.updated's job — but it does need to fire the
	// dunning email exactly once per event. Dedup via the same table.
	if ev.Type == "invoice.payment_failed" {
		if ev.ID != "" {
			fresh, err := h.accounts.RecordStripeEvent(r.Context(), ev.ID)
			if err != nil {
				http.Error(w, "dedup write failed", http.StatusInternalServerError)
				return
			}
			if !fresh {
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		h.sendPaymentFailedEmail(r.Context(), ev.CustomerID)
		w.WriteHeader(http.StatusOK)
		return
	}

	if !billing.IsSubscriptionEvent(ev.Type) {
		w.WriteHeader(http.StatusOK) // acknowledged, nothing to do
		return
	}

	// Dedup: Stripe retries 5xx for ~3 days and may also replay older events.
	// First-seen passes through; a duplicate exits as 200 OK without touching
	// account state. Missing event.id (shouldn't happen on real Stripe events)
	// falls through — better to process than to wedge.
	if ev.ID != "" {
		fresh, err := h.accounts.RecordStripeEvent(r.Context(), ev.ID)
		if err != nil {
			http.Error(w, "dedup write failed", http.StatusInternalServerError)
			return
		}
		if !fresh {
			h.logger().Debug("stripe webhook duplicate ignored", "event_id", ev.ID, "type", ev.Type)
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	// Deletion is its own write — it must also extend trial_ends_at so the
	// owner isn't instantly locked out and CAN start a new Checkout.
	if ev.Type == "customer.subscription.deleted" {
		graceUntil := time.Now().UTC().Add(postCancellationGrace)
		if err := h.accounts.HandleSubscriptionCanceled(r.Context(), ev.CustomerID, ev.Created, graceUntil); err != nil {
			http.Error(w, "sync failed", http.StatusInternalServerError)
			return
		}
		h.logger().Info("stripe subscription canceled",
			"event", ev.Type, "customer", ev.CustomerID, "grace_until", graceUntil)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Pick the plan from the first item that maps to a known plan price.
	// Stripe's items.data order is indeterminate — a Pro sub with a seat
	// add-on can return the add-on first; we'd then demote the customer.
	plan := billing.PlanTrial
	for _, priceID := range ev.PriceIDs {
		if p := h.billing.PlanForPrice(priceID); p != billing.PlanTrial {
			plan = p
			break
		}
	}

	// Do NOT downgrade past_due / unpaid / incomplete accounts to the trial
	// plan here. Stripe retries a failed invoice for ~23 days before firing
	// customer.subscription.deleted, and during that window the customer is
	// still ours — silently switching them to "trial" makes the layout tell
	// a paying customer "your free trial has ended." We keep them on their
	// paid plan and let SubscriptionStatus (SyncSubscription writes it) carry
	// the "payment issue" signal to the banners / mailer. On
	// subscription.deleted (the terminal cancel) HandleSubscriptionCanceled
	// drops the plan and extends the grace window.

	if err := h.accounts.SyncSubscription(r.Context(), ev.CustomerID, ev.SubscriptionID, ev.Status, plan, ev.Created); err != nil {
		// 5xx so Stripe retries the delivery.
		http.Error(w, "sync failed", http.StatusInternalServerError)
		return
	}
	h.logger().Info("stripe subscription synced",
		"event", ev.Type, "customer", ev.CustomerID, "plan", plan, "status", ev.Status)
	w.WriteHeader(http.StatusOK)
}
