// Package billing wraps Stripe. Phase 3 only creates a Stripe Customer per
// account; Checkout, webhooks, and plan enforcement land in later phases.
//
// The package degrades to a Noop implementation when STRIPE_SECRET_KEY is
// unset so local dev and CI work without a Stripe account. Selection
// happens at server startup; handlers always go through the Customers
// interface.
package billing

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Customers is the per-account Stripe operations the rest of the app cares
// about in Phase 3. Adding here forces every billing call site to be
// testable.
type Customers interface {
	// Create issues a Stripe Customer for the account and returns its ID.
	// Implementations should be idempotent on the account UUID (use it as
	// the Stripe idempotency key) so duplicate calls don't create
	// duplicate customers.
	Create(ctx context.Context, accountID uuid.UUID, email, name string) (string, error)
	// Enabled reports whether the implementation talks to a real Stripe.
	// Used by the settings page so we can render "not configured" without
	// hitting the network.
	Enabled() bool
}

// NoopCustomers is the dev/CI fallback. Returns "" and nil; the settings
// page knows to render "Stripe not configured" when Enabled() is false.
type NoopCustomers struct{}

func (NoopCustomers) Create(_ context.Context, _ uuid.UUID, _, _ string) (string, error) {
	return "", nil
}
func (NoopCustomers) Enabled() bool { return false }

// StripeCustomers calls Stripe's REST API directly via net/http. We don't
// pull in the stripe-go SDK in Phase 3 because all we need is one endpoint;
// dragging in the SDK now would commit us to its version for later phases
// before we know which features (webhooks, subscriptions, prorations)
// matter.
type StripeCustomers struct {
	apiKey string
	http   *http.Client
	base   string
}

// NewStripeCustomers returns a real Stripe client. apiKey is your
// sk_test_... or sk_live_... key. If apiKey is empty, returns a
// NoopCustomers so callers don't have to branch on env at every site.
func NewStripeCustomers(apiKey string) Customers {
	if apiKey == "" {
		return NoopCustomers{}
	}
	return &StripeCustomers{
		apiKey: apiKey,
		http:   &http.Client{Timeout: 10 * time.Second},
		base:   "https://api.stripe.com/v1",
	}
}

func (s *StripeCustomers) Enabled() bool { return true }

// Create posts to POST /v1/customers with the account UUID as the
// idempotency key, so a retry of a half-failed call doesn't create a
// duplicate customer.
//
// We don't parse the full response — just the id. Errors below 500 are
// treated as fatal (4xx Stripe errors usually mean misconfiguration);
// transient 5xx bubble up so the caller can decide whether to retry.
func (s *StripeCustomers) Create(ctx context.Context, accountID uuid.UUID, email, name string) (string, error) {
	form := url.Values{}
	form.Set("email", email)
	if name != "" {
		form.Set("name", name)
	}
	form.Set("metadata[account_id]", accountID.String())

	req, err := http.NewRequestWithContext(ctx, "POST", s.base+"/customers",
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Stripe-Idempotency-Key dedupes retries server-side for 24h.
	req.Header.Set("Idempotency-Key", "account-create-"+accountID.String())

	resp, err := s.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", errors.New("stripe: customer create failed: " + resp.Status)
	}
	id, err := decodeCustomerID(resp.Body)
	if err != nil {
		return "", err
	}
	return id, nil
}

// decodeCustomerID extracts the "id" field from Stripe's JSON response. We
// stay in the standard library to avoid a third-party JSON helper.
func decodeCustomerID(body interface{ Read([]byte) (int, error) }) (string, error) {
	type customerResp struct {
		ID string `json:"id"`
	}
	var c customerResp
	if err := jsonDecode(body, &c); err != nil {
		return "", err
	}
	if c.ID == "" {
		return "", errors.New("stripe: empty id in response")
	}
	return c.ID, nil
}
