// Package billing wraps Stripe for subscription billing: customer creation,
// Checkout, the Customer Portal, and webhook event verification + parsing.
// It talks to Stripe's REST API directly over net/http — no SDK — so the
// dependency surface stays small and the version isn't pinned for us.
//
// Without a Stripe secret key the package degrades to a no-op so dev and CI
// run with no Stripe account; callers always go through the Service
// interface and never branch on configuration themselves.
package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Plan tiers — these match the accounts.plan CHECK constraint.
const (
	PlanTrial   = "trial"
	PlanStarter = "starter"
	PlanPro     = "pro"
)

// CheckoutInput describes a subscription Checkout Session to create.
type CheckoutInput struct {
	CustomerID string
	PriceID    string
	SuccessURL string
	CancelURL  string
}

// Service is the billing surface the rest of the app depends on. It is
// implemented by a real Stripe client and by a no-op fallback.
type Service interface {
	// Create issues a Stripe Customer for the account — idempotent on the
	// account UUID — and returns its ID.
	Create(ctx context.Context, accountID uuid.UUID, email, name string) (string, error)
	// Checkout creates a subscription Checkout Session and returns the URL
	// the customer should be redirected to.
	Checkout(ctx context.Context, in CheckoutInput) (string, error)
	// Portal creates a Stripe Customer Portal session and returns its URL.
	Portal(ctx context.Context, customerID, returnURL string) (string, error)
	// PriceID returns the configured Stripe Price for a plan tier, or "".
	PriceID(plan string) string
	// PlanForPrice maps a Stripe Price ID back to a plan tier, defaulting
	// to PlanTrial when the price is not recognised.
	PlanForPrice(priceID string) string
	// VerifyWebhook checks a Stripe-Signature header against the raw body.
	VerifyWebhook(payload []byte, sigHeader string) error
	// ListCharges returns a customer's recent charges, newest first.
	ListCharges(ctx context.Context, customerID string) ([]Charge, error)
	// Refund issues a full refund of a charge. idempotencyKey makes a
	// retried call safe.
	Refund(ctx context.Context, chargeID, idempotencyKey string) (RefundResult, error)
	// Enabled reports whether a real Stripe is wired.
	Enabled() bool
}

// Config holds the Stripe credentials and price IDs.
type Config struct {
	SecretKey     string // sk_test_... / sk_live_...
	WebhookSecret string // whsec_... — webhook signing secret
	PriceStarter  string // price_... for the Starter plan
	PricePro      string // price_... for the Pro plan
}

// New returns a Service. With an empty SecretKey it returns a no-op so
// callers don't have to branch on configuration.
func New(c Config) Service {
	if c.SecretKey == "" {
		return noopService{}
	}
	return &stripeService{
		cfg:  c,
		http: &http.Client{Timeout: 10 * time.Second},
		base: "https://api.stripe.com/v1",
	}
}

// --- no-op fallback ---

type noopService struct{}

func (noopService) Create(context.Context, uuid.UUID, string, string) (string, error) {
	return "", nil
}
func (noopService) Checkout(context.Context, CheckoutInput) (string, error) {
	return "", errors.New("billing: Stripe is not configured")
}
func (noopService) Portal(context.Context, string, string) (string, error) {
	return "", errors.New("billing: Stripe is not configured")
}
func (noopService) PriceID(string) string      { return "" }
func (noopService) PlanForPrice(string) string { return PlanTrial }
func (noopService) VerifyWebhook([]byte, string) error {
	return errors.New("billing: Stripe is not configured")
}
func (noopService) Enabled() bool { return false }

// --- real Stripe client ---

type stripeService struct {
	cfg  Config
	http *http.Client
	base string
}

func (s *stripeService) Enabled() bool { return true }

func (s *stripeService) PriceID(plan string) string {
	switch plan {
	case PlanStarter:
		return s.cfg.PriceStarter
	case PlanPro:
		return s.cfg.PricePro
	default:
		return ""
	}
}

func (s *stripeService) PlanForPrice(priceID string) string {
	switch priceID {
	case s.cfg.PriceStarter:
		return PlanStarter
	case s.cfg.PricePro:
		return PlanPro
	default:
		return PlanTrial
	}
}

// Create posts to POST /v1/customers with the account UUID as the
// idempotency key, so a retry of a half-failed call doesn't create a
// duplicate customer.
func (s *stripeService) Create(ctx context.Context, accountID uuid.UUID, email, name string) (string, error) {
	form := url.Values{}
	form.Set("email", email)
	if name != "" {
		form.Set("name", name)
	}
	form.Set("metadata[account_id]", accountID.String())
	resp, err := s.post(ctx, "/customers", form, "account-create-"+accountID.String())
	if err != nil {
		return "", err
	}
	id, _ := resp["id"].(string)
	if id == "" {
		return "", errors.New("stripe: empty id in customer response")
	}
	return id, nil
}

// Checkout creates a subscription-mode Checkout Session. Stripe hosts the
// payment page — no card data touches our servers.
func (s *stripeService) Checkout(ctx context.Context, in CheckoutInput) (string, error) {
	if in.CustomerID == "" || in.PriceID == "" {
		return "", errors.New("billing: checkout needs a customer and a price")
	}
	form := url.Values{}
	form.Set("mode", "subscription")
	form.Set("customer", in.CustomerID)
	form.Set("line_items[0][price]", in.PriceID)
	form.Set("line_items[0][quantity]", "1")
	form.Set("success_url", in.SuccessURL)
	form.Set("cancel_url", in.CancelURL)
	resp, err := s.post(ctx, "/checkout/sessions", form, "")
	if err != nil {
		return "", err
	}
	return sessionURL(resp)
}

// Portal creates a Customer Portal session where the customer can change or
// cancel their subscription and download invoices.
func (s *stripeService) Portal(ctx context.Context, customerID, returnURL string) (string, error) {
	if customerID == "" {
		return "", errors.New("billing: portal needs a customer")
	}
	form := url.Values{}
	form.Set("customer", customerID)
	form.Set("return_url", returnURL)
	resp, err := s.post(ctx, "/billing_portal/sessions", form, "")
	if err != nil {
		return "", err
	}
	return sessionURL(resp)
}

func sessionURL(resp map[string]any) (string, error) {
	u, _ := resp["url"].(string)
	if u == "" {
		return "", errors.New("stripe: session response had no url")
	}
	return u, nil
}

// post issues a form-encoded POST to the Stripe API and decodes the JSON
// response. idempotencyKey, when non-empty, is sent as Idempotency-Key.
func (s *stripeService) post(ctx context.Context, path string, form url.Values, idempotencyKey string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+path,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.SecretKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	return s.do(req, path)
}

// do executes a prepared Stripe request and decodes the JSON response.
// Shared by post and get.
func (s *stripeService) do(req *http.Request, path string) (map[string]any, error) {
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("stripe: %s %s: %s", path, resp.Status, stripeErr(body))
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("stripe: decode %s response: %w", path, err)
	}
	return out, nil
}

// stripeErr pulls the human-readable message out of a Stripe error body.
func stripeErr(body []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	return "request failed"
}
