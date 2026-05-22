package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestReportUsage(t *testing.T) {
	var gotForm url.Values
	var gotIdem, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm, gotIdem, gotPath = r.PostForm, r.Header.Get("Idempotency-Key"), r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "billing.meter_event"})
	}))
	defer srv.Close()

	s := &stripeService{
		cfg:  Config{SecretKey: "sk_test_x", MeterEvent: "drill_run"},
		http: srv.Client(), base: srv.URL,
	}
	if err := s.ReportUsage(context.Background(), "cus_1", "drill-abc"); err != nil {
		t.Fatalf("ReportUsage: %v", err)
	}
	if gotPath != "/billing/meter_events" {
		t.Errorf("path = %q, want /billing/meter_events", gotPath)
	}
	if gotForm.Get("event_name") != "drill_run" {
		t.Errorf("event_name = %q", gotForm.Get("event_name"))
	}
	if gotForm.Get("payload[stripe_customer_id]") != "cus_1" {
		t.Errorf("customer = %q", gotForm.Get("payload[stripe_customer_id]"))
	}
	if gotForm.Get("payload[value]") != "1" {
		t.Errorf("value = %q, want 1", gotForm.Get("payload[value]"))
	}
	if gotForm.Get("identifier") != "drill-abc" {
		t.Errorf("identifier = %q", gotForm.Get("identifier"))
	}
	if gotIdem != "drill-abc" {
		t.Errorf("idempotency key = %q, want drill-abc", gotIdem)
	}
}

func TestReportUsageNoMeterIsNoop(t *testing.T) {
	// No MeterEvent and no base URL: if ReportUsage tried to call Stripe it
	// would fail — so a nil error proves it short-circuited.
	s := &stripeService{cfg: Config{SecretKey: "sk_test_x"}}
	if err := s.ReportUsage(context.Background(), "cus_1", "id"); err != nil {
		t.Fatalf("no-meter ReportUsage should be a silent no-op: %v", err)
	}
}

func TestReportUsageRequiresCustomer(t *testing.T) {
	s := &stripeService{cfg: Config{SecretKey: "sk_test_x", MeterEvent: "drill_run"}}
	if err := s.ReportUsage(context.Background(), "", "id"); err == nil {
		t.Error("ReportUsage with no customer should error")
	}
}

func TestReportUsageNoopService(t *testing.T) {
	if err := (noopService{}).ReportUsage(context.Background(), "cus_1", "id"); err != nil {
		t.Errorf("noop ReportUsage should be nil: %v", err)
	}
}
