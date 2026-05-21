package analytics

import (
	"io"
	"log/slog"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewFallsBackToNoop(t *testing.T) {
	a := New("", "", discardLogger())
	if _, ok := a.(*NoopAnalytics); !ok {
		t.Fatalf("empty API key should yield NoopAnalytics, got %T", a)
	}
	if a.Enabled() {
		t.Error("NoopAnalytics should report Enabled() == false")
	}
}

func TestNewWithKeyIsEnabled(t *testing.T) {
	a := New("phc_testkey", "", discardLogger())
	if _, ok := a.(*PostHogAnalytics); !ok {
		t.Fatalf("a non-empty key should yield PostHogAnalytics, got %T", a)
	}
	if !a.Enabled() {
		t.Error("PostHogAnalytics should report Enabled() == true")
	}
}

func TestNoopCaptureDoesNotPanic(t *testing.T) {
	a := NewNoopAnalytics(discardLogger())
	// Capture must be safe with any inputs, including nil props.
	a.Capture("user-123", EventSignedUp, nil)
	a.Capture("user-123", EventDrillCompleted, map[string]any{"drill_id": "d1"})
}
