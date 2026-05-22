package billing

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"
)

// Charge is a payment a customer made — the staff refund view lists these.
type Charge struct {
	ID          string
	Amount      int64 // in the currency's smallest unit (e.g. cents)
	Currency    string
	Created     time.Time
	Status      string // succeeded, pending, failed
	Refunded    bool   // fully refunded already
	Description string
}

// RefundResult is the outcome of a Refund call.
type RefundResult struct {
	ID     string
	Amount int64
	Status string // succeeded, pending, failed, canceled
}

// ListCharges returns a customer's recent charges, newest first. Used by the
// staff admin panel to pick a charge to refund.
func (s *stripeService) ListCharges(ctx context.Context, customerID string) ([]Charge, error) {
	if customerID == "" {
		return nil, errors.New("billing: list charges needs a customer")
	}
	resp, err := s.get(ctx, "/charges?limit=20&customer="+url.QueryEscape(customerID))
	if err != nil {
		return nil, err
	}
	data, _ := resp["data"].([]any)
	out := make([]Charge, 0, len(data))
	for _, item := range data {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, parseCharge(m))
	}
	return out, nil
}

// Refund issues a full refund of a charge. idempotencyKey, when non-empty,
// makes a retried call safe — a duplicate request returns the first refund
// rather than issuing a second.
func (s *stripeService) Refund(ctx context.Context, chargeID, idempotencyKey string) (RefundResult, error) {
	if chargeID == "" {
		return RefundResult{}, errors.New("billing: refund needs a charge")
	}
	form := url.Values{}
	form.Set("charge", chargeID)
	resp, err := s.post(ctx, "/refunds", form, idempotencyKey)
	if err != nil {
		return RefundResult{}, err
	}
	r := RefundResult{}
	r.ID, _ = resp["id"].(string)
	r.Status, _ = resp["status"].(string)
	if f, ok := resp["amount"].(float64); ok {
		r.Amount = int64(f)
	}
	if r.ID == "" {
		return RefundResult{}, errors.New("stripe: empty id in refund response")
	}
	return r, nil
}

// parseCharge maps a decoded Stripe charge object to a Charge. JSON numbers
// decode as float64; Stripe sends `created` as a Unix timestamp.
func parseCharge(m map[string]any) Charge {
	c := Charge{}
	c.ID, _ = m["id"].(string)
	c.Currency, _ = m["currency"].(string)
	c.Status, _ = m["status"].(string)
	c.Description, _ = m["description"].(string)
	c.Refunded, _ = m["refunded"].(bool)
	if f, ok := m["amount"].(float64); ok {
		c.Amount = int64(f)
	}
	if f, ok := m["created"].(float64); ok {
		c.Created = time.Unix(int64(f), 0).UTC()
	}
	return c
}

// get issues an authenticated GET to the Stripe API and decodes the JSON
// response. It shares error handling with post.
func (s *stripeService) get(ctx context.Context, path string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.SecretKey)
	return s.do(req, path)
}

// --- no-op fallback ---

func (noopService) ListCharges(context.Context, string) ([]Charge, error) {
	return nil, errors.New("billing: Stripe is not configured")
}

func (noopService) Refund(context.Context, string, string) (RefundResult, error) {
	return RefundResult{}, errors.New("billing: Stripe is not configured")
}
