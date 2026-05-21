package obs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestReadinessHandler(t *testing.T) {
	ok := ReadinessHandler(map[string]func(context.Context) error{
		"database": func(context.Context) error { return nil },
	})
	rec := httptest.NewRecorder()
	ok(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("all-checks-pass: got %d, want 200", rec.Code)
	}

	bad := ReadinessHandler(map[string]func(context.Context) error{
		"database": func(context.Context) error { return errors.New("connection refused") },
	})
	rec = httptest.NewRecorder()
	bad(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("failing-check: got %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "database") {
		t.Fatalf("503 body should name the failing check, got %q", rec.Body.String())
	}
}

// capturingReporter records what the Recoverer hands it.
type capturingReporter struct {
	panics int
	errs   int
}

func (c *capturingReporter) CaptureError(context.Context, error, map[string]string) { c.errs++ }
func (c *capturingReporter) CapturePanic(context.Context, any, map[string]string)   { c.panics++ }
func (c *capturingReporter) Flush(time.Duration)                                    {}

func TestRecovererReportsPanic(t *testing.T) {
	cap := &capturingReporter{}
	p := &Provider{Reporter: cap}

	h := p.Recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panicking handler: got %d, want 500", rec.Code)
	}
	if cap.panics != 1 {
		t.Fatalf("reporter saw %d panics, want 1", cap.panics)
	}
}

func TestRecovererPassesThroughOK(t *testing.T) {
	cap := &capturingReporter{}
	p := &Provider{Reporter: cap}
	h := p.Recoverer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || cap.panics != 0 {
		t.Fatalf("clean handler: code=%d panics=%d", rec.Code, cap.panics)
	}
}

func TestNoopReporterLogsError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	r := NewNoopReporter(logger)
	r.CaptureError(context.Background(), errors.New("disk full"), map[string]string{"area": "evidence"})
	if !strings.Contains(buf.String(), "disk full") {
		t.Fatalf("NoopReporter should log the error, got %q", buf.String())
	}
}

func TestNewErrorReporterFallsBackToNoop(t *testing.T) {
	r, err := NewErrorReporter("", "dev", discardLogger())
	if err != nil {
		t.Fatalf("NewErrorReporter: %v", err)
	}
	if _, ok := r.(*NoopReporter); !ok {
		t.Fatalf("empty DSN should yield a NoopReporter, got %T", r)
	}
}
