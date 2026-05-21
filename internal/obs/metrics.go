package obs

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds the application's Prometheus instruments. One instance is
// created at startup and shared; all instruments are safe for concurrent use.
type Metrics struct {
	reg *prometheus.Registry

	httpRequests *prometheus.CounterVec
	httpDuration *prometheus.HistogramVec

	drillsTotal   *prometheus.CounterVec
	drillDuration prometheus.Histogram

	webhookDeliveries *prometheus.CounterVec

	queueDepth prometheus.Gauge
}

// NewMetrics builds the registry and registers every instrument.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		reg: reg,
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "HTTP requests by route and status.",
		}, []string{"method", "route", "status"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency by route.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route"}),
		drillsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "drills_total",
			Help: "Drills by terminal status.",
		}, []string{"status"}),
		drillDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "drill_duration_seconds",
			Help:    "End-to-end drill duration.",
			Buckets: []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800},
		}),
		webhookDeliveries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "webhook_deliveries_total",
			Help: "Webhook delivery attempts by outcome.",
		}, []string{"outcome"}),
		queueDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "river_jobs_available",
			Help: "River jobs in the available state (queue depth).",
		}),
	}
	reg.MustRegister(
		m.httpRequests, m.httpDuration,
		m.drillsTotal, m.drillDuration,
		m.webhookDeliveries, m.queueDepth,
	)
	return m
}

// Handler serves the Prometheus exposition format for the /metrics route.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// Middleware records request count + latency for every HTTP request.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)

		route := routePattern(r)
		m.httpRequests.WithLabelValues(r.Method, route, strconv.Itoa(ww.Status())).Inc()
		m.httpDuration.WithLabelValues(route).Observe(time.Since(start).Seconds())
	})
}

// RecordDrill records a drill's terminal status and (when known) duration.
func (m *Metrics) RecordDrill(status string, duration time.Duration) {
	m.drillsTotal.WithLabelValues(status).Inc()
	if duration > 0 {
		m.drillDuration.Observe(duration.Seconds())
	}
}

// RecordWebhookDelivery records a webhook delivery outcome ("delivered" or
// "failed").
func (m *Metrics) RecordWebhookDelivery(outcome string) {
	m.webhookDeliveries.WithLabelValues(outcome).Inc()
}

// SetQueueDepth updates the River queue-depth gauge.
func (m *Metrics) SetQueueDepth(n int) {
	m.queueDepth.Set(float64(n))
}
