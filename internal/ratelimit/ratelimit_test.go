package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBucketExhaustsAndRefills(t *testing.T) {
	l := New(60, 3) // 1 token/sec, burst 3
	now := time.Now()
	l.now = func() time.Time { return now }

	// Burst of 3 allowed.
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("k"); !ok {
			t.Fatalf("request %d should be allowed within burst", i+1)
		}
	}
	// 4th denied.
	ok, retryAfter := l.Allow("k")
	if ok {
		t.Fatal("4th request should be denied")
	}
	if retryAfter <= 0 {
		t.Fatalf("retryAfter should be positive, got %v", retryAfter)
	}

	// After 1 second, one token refills.
	now = now.Add(time.Second)
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("request should be allowed after 1s refill")
	}
	if ok, _ := l.Allow("k"); ok {
		t.Fatal("only one token should have refilled")
	}
}

func TestKeysAreIndependent(t *testing.T) {
	l := New(60, 1)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("a's first request should pass")
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("b's first request should pass — separate bucket")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("a's second request should be denied")
	}
}

func TestMiddleware429(t *testing.T) {
	l := New(60, 1)
	key := func(*http.Request) string { return "fixed" }
	h := l.Middleware(key)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: got %d, want 200", rec1.Code)
	}

	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: got %d, want 429", rec2.Code)
	}
	if rec2.Header().Get("Retry-After") == "" {
		t.Fatal("429 response should carry a Retry-After header")
	}
}

func TestSweepEvictsIdleBuckets(t *testing.T) {
	l := New(60, 5)
	now := time.Now()
	l.now = func() time.Time { return now }

	l.Allow("stale")
	now = now.Add(time.Hour)
	l.Allow("fresh")

	l.sweep(30 * time.Minute)

	l.mu.Lock()
	_, staleExists := l.buckets["stale"]
	_, freshExists := l.buckets["fresh"]
	l.mu.Unlock()

	if staleExists {
		t.Error("stale bucket should have been evicted")
	}
	if !freshExists {
		t.Error("fresh bucket should survive the sweep")
	}
}
