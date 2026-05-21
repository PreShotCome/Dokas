package obs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics scrape: got %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

func TestMetricsMiddlewareCountsRequests(t *testing.T) {
	m := NewMetrics()
	h := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for i := 0; i < 3; i++ {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	}

	body := scrape(t, m)
	if !strings.Contains(body, "http_requests_total") {
		t.Fatalf("/metrics missing http_requests_total:\n%s", body)
	}
	if !strings.Contains(body, "http_request_duration_seconds") {
		t.Fatalf("/metrics missing the latency histogram")
	}
}

func TestRecordDrillMetric(t *testing.T) {
	m := NewMetrics()
	m.RecordDrill("succeeded", 12*time.Second)
	m.RecordDrill("failed", 3*time.Second)

	body := scrape(t, m)
	if !strings.Contains(body, `drills_total{status="succeeded"} 1`) {
		t.Fatalf("expected a succeeded drill counter:\n%s", body)
	}
	if !strings.Contains(body, `drills_total{status="failed"} 1`) {
		t.Fatalf("expected a failed drill counter:\n%s", body)
	}
	if !strings.Contains(body, "drill_duration_seconds") {
		t.Fatalf("expected the drill duration histogram")
	}
}

func TestRecordWebhookAndQueueDepth(t *testing.T) {
	m := NewMetrics()
	m.RecordWebhookDelivery("delivered")
	m.RecordWebhookDelivery("delivered")
	m.RecordWebhookDelivery("failed")
	m.SetQueueDepth(7)

	body := scrape(t, m)
	if !strings.Contains(body, `webhook_deliveries_total{outcome="delivered"} 2`) {
		t.Fatalf("expected 2 delivered webhooks:\n%s", body)
	}
	if !strings.Contains(body, "river_jobs_available 7") {
		t.Fatalf("expected queue depth gauge = 7:\n%s", body)
	}
}
