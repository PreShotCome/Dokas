// Package ratelimit provides an in-process token-bucket limiter and HTTP
// middleware. Single-binary deployment means no Redis: buckets live in a map
// guarded by a mutex, with a background sweep to evict idle keys.
package ratelimit

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Limiter is a keyed token-bucket rate limiter. Each key (an IP, an account
// ID, etc.) gets its own bucket that refills at `rate` tokens per second up
// to `burst` capacity.
type Limiter struct {
	rate  float64 // tokens per second
	burst float64 // max tokens

	mu      sync.Mutex
	buckets map[string]*bucket
	now     func() time.Time // injectable for tests
}

type bucket struct {
	tokens   float64
	lastSeen time.Time
}

// New returns a limiter that allows `burst` requests immediately and then
// refills at `perMinute` requests per minute.
func New(perMinute, burst int) *Limiter {
	l := &Limiter{
		rate:    float64(perMinute) / 60.0,
		burst:   float64(burst),
		buckets: make(map[string]*bucket),
		now:     time.Now,
	}
	return l
}

// Allow consumes one token for key. Returns ok=false when the bucket is
// empty, plus the duration the caller should advise via Retry-After.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	b, exists := l.buckets[key]
	if !exists {
		b = &bucket{tokens: l.burst, lastSeen: now}
		l.buckets[key] = b
	} else {
		elapsed := now.Sub(b.lastSeen).Seconds()
		b.tokens = minF(l.burst, b.tokens+elapsed*l.rate)
		b.lastSeen = now
	}

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	// Tokens needed to reach 1, converted to wait time.
	deficit := 1 - b.tokens
	wait := time.Duration(deficit / l.rate * float64(time.Second))
	return false, wait
}

// sweep evicts buckets idle longer than ttl so the map can't grow unbounded
// under a flood of distinct keys.
func (l *Limiter) sweep(ttl time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := l.now().Add(-ttl)
	for k, b := range l.buckets {
		if b.lastSeen.Before(cutoff) {
			delete(l.buckets, k)
		}
	}
}

// StartSweeper runs the eviction loop until stop is closed. Call once per
// limiter at startup.
func (l *Limiter) StartSweeper(stop <-chan struct{}, every, ttl time.Duration) {
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				l.sweep(ttl)
			}
		}
	}()
}

// KeyFunc derives the bucket key from a request.
type KeyFunc func(*http.Request) string

// Middleware rejects requests with 429 + Retry-After when the limiter is
// exhausted for the request's key.
func (l *Limiter) Middleware(key KeyFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ok, retryAfter := l.Allow(key(r))
			if !ok {
				secs := int(retryAfter.Seconds())
				if secs < 1 {
					secs = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
