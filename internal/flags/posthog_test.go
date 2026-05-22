package flags

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// waitFor polls until cond is true or the deadline passes.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

func TestPostHogFlagsEvaluatesAndCaches(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = io.WriteString(w, `{"featureFlags":{"self_serve_signup":false}}`)
	}))
	defer srv.Close()

	f := NewPostHogFlags("phc_test", srv.URL, slog.New(slog.DiscardHandler))
	ctx := context.Background()

	// First call is a cache miss: it serves the static default (true) and
	// kicks a background refresh.
	if !f.Enabled(ctx, SelfServeSignup, "ip-1") {
		t.Fatal("first call should fall back to the static default (true)")
	}
	// Once the refresh lands, PostHog's value (false) wins.
	waitFor(t, func() bool { return !f.Enabled(ctx, SelfServeSignup, "ip-1") })

	// A flag PostHog doesn't define still falls through to the static layer.
	if f.Enabled(ctx, "unknown_flag", "ip-1") {
		t.Error("an unknown flag should be false via the static fallback")
	}
}

func TestPostHogFlagsFallsBackOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := NewPostHogFlags("phc_test", srv.URL, slog.New(slog.DiscardHandler))
	ctx := context.Background()

	// PostHog errors out; the static default must hold.
	for i := 0; i < 5; i++ {
		if !f.Enabled(ctx, SelfServeSignup, "ip-2") {
			t.Fatal("a PostHog error must not override the static default")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
