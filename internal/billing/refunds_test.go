package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListChargesAndRefund(t *testing.T) {
	var gotRefundCharge, gotIdempotency string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk_test_x" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/charges":
			if got := r.URL.Query().Get("customer"); got != "cus_123" {
				t.Errorf("customer param = %q, want cus_123", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": "list",
				"data": []map[string]any{
					{"id": "ch_1", "amount": 1500, "currency": "usd",
						"created": 1700000000, "status": "succeeded",
						"refunded": false, "description": "Starter plan"},
					{"id": "ch_2", "amount": 500, "currency": "usd",
						"created": 1690000000, "status": "succeeded", "refunded": true},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/refunds":
			_ = r.ParseForm()
			gotRefundCharge = r.PostFormValue("charge")
			gotIdempotency = r.Header.Get("Idempotency-Key")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "re_1", "amount": 1500, "status": "succeeded",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	s := &stripeService{cfg: Config{SecretKey: "sk_test_x"}, http: srv.Client(), base: srv.URL}

	charges, err := s.ListCharges(context.Background(), "cus_123")
	if err != nil {
		t.Fatalf("ListCharges: %v", err)
	}
	if len(charges) != 2 {
		t.Fatalf("got %d charges, want 2", len(charges))
	}
	if charges[0].ID != "ch_1" || charges[0].Amount != 1500 || charges[0].Currency != "usd" {
		t.Errorf("charge 0 = %+v", charges[0])
	}
	if charges[0].Created.IsZero() {
		t.Error("charge 0 created timestamp not parsed")
	}
	if !charges[1].Refunded {
		t.Error("charge 1 should be marked refunded")
	}

	res, err := s.Refund(context.Background(), "ch_1", "idem-1")
	if err != nil {
		t.Fatalf("Refund: %v", err)
	}
	if gotRefundCharge != "ch_1" {
		t.Errorf("refund charge param = %q, want ch_1", gotRefundCharge)
	}
	if gotIdempotency != "idem-1" {
		t.Errorf("idempotency key = %q, want idem-1", gotIdempotency)
	}
	if res.ID != "re_1" || res.Amount != 1500 || res.Status != "succeeded" {
		t.Errorf("refund result = %+v", res)
	}
}

func TestRefundStripeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "Charge ch_x has already been refunded."},
		})
	}))
	defer srv.Close()
	s := &stripeService{cfg: Config{SecretKey: "sk_test_x"}, http: srv.Client(), base: srv.URL}
	if _, err := s.Refund(context.Background(), "ch_x", ""); err == nil {
		t.Fatal("expected an error for an already-refunded charge")
	}
}

func TestRefundsNoopErrors(t *testing.T) {
	if _, err := (noopService{}).Refund(context.Background(), "ch_1", ""); err == nil {
		t.Error("noop Refund should error")
	}
	if _, err := (noopService{}).ListCharges(context.Background(), "cus_1"); err == nil {
		t.Error("noop ListCharges should error")
	}
}

func TestRefundRequiresCharge(t *testing.T) {
	s := &stripeService{cfg: Config{SecretKey: "sk_test_x"}}
	if _, err := s.Refund(context.Background(), "", ""); err == nil {
		t.Error("Refund with no charge should error")
	}
}
