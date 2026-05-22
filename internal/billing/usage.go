package billing

import (
	"context"
	"errors"
	"net/url"
)

// ReportUsage records one unit of billable usage — a single drill run — for
// a customer, as a Stripe Billing Meter event. The configured metered price
// decides any included allowance and the per-unit rate; the app only reports
// raw usage.
//
// identifier makes a retried call safe: Stripe drops a meter event whose
// identifier it has already seen, so reporting the same drill twice counts
// once. It is a silent no-op when no meter is configured, so usage reporting
// can be left off without callers branching.
func (s *stripeService) ReportUsage(ctx context.Context, customerID, identifier string) error {
	if s.cfg.MeterEvent == "" {
		return nil
	}
	if customerID == "" {
		return errors.New("billing: report usage needs a customer")
	}
	form := url.Values{}
	form.Set("event_name", s.cfg.MeterEvent)
	form.Set("payload[stripe_customer_id]", customerID)
	form.Set("payload[value]", "1")
	if identifier != "" {
		form.Set("identifier", identifier)
	}
	_, err := s.post(ctx, "/billing/meter_events", form, identifier)
	return err
}

func (noopService) ReportUsage(context.Context, string, string) error { return nil }
