// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package flags

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// decideCacheTTL is how long a PostHog flag evaluation is cached per distinct
// ID before a background refresh is triggered.
const decideCacheTTL = 60 * time.Second

// PostHogFlags evaluates feature flags against PostHog's /decide endpoint.
//
// Enabled never blocks on the network: it serves the cached evaluation,
// refreshes it in the background when stale, and falls back to the static
// env-driven defaults whenever PostHog has no answer (yet, or on error). A
// flag is therefore at most one request stale on first use — fine for
// feature gating, and it keeps every request fast.
type PostHogFlags struct {
	endpoint string
	apiKey   string
	http     *http.Client
	fallback Flags
	logger   *slog.Logger

	mu       sync.Mutex
	cache    map[string]flagSet
	inflight map[string]bool
}

type flagSet struct {
	flags    map[string]bool
	variants map[string]string
	fetched  time.Time
}

// NewPostHogFlags builds a PostHog-backed flag evaluator. host defaults to
// https://app.posthog.com when empty.
func NewPostHogFlags(apiKey, host string, logger *slog.Logger) *PostHogFlags {
	if host == "" {
		host = "https://app.posthog.com"
	}
	return &PostHogFlags{
		endpoint: strings.TrimRight(host, "/") + "/decide/?v=3",
		apiKey:   apiKey,
		http:     &http.Client{Timeout: 10 * time.Second},
		fallback: NewStaticFlags(),
		logger:   logger,
		cache:    map[string]flagSet{},
		inflight: map[string]bool{},
	}
}

// lookup returns the cached flag set for a distinct ID, kicking off a
// background refresh when the entry is missing or stale.
func (p *PostHogFlags) lookup(distinctID string) (flagSet, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.cache[distinctID]
	if (!ok || time.Since(entry.fetched) > decideCacheTTL) && !p.inflight[distinctID] {
		p.inflight[distinctID] = true
		go p.refresh(distinctID)
	}
	return entry, ok
}

// Enabled returns the cached PostHog value for key/distinctID, refreshing in
// the background when stale, and falls through to the static default when
// PostHog has not (or cannot) answer.
func (p *PostHogFlags) Enabled(ctx context.Context, key, distinctID string) bool {
	if entry, ok := p.lookup(distinctID); ok {
		if v, present := entry.flags[key]; present {
			return v
		}
	}
	return p.fallback.Enabled(ctx, key, distinctID)
}

// Variant returns the cached PostHog multivariate variant for key/distinctID,
// falling through to the static default when PostHog has no answer.
func (p *PostHogFlags) Variant(ctx context.Context, key, distinctID string) string {
	if entry, ok := p.lookup(distinctID); ok {
		if v, present := entry.variants[key]; present {
			return v
		}
	}
	return p.fallback.Variant(ctx, key, distinctID)
}

func (p *PostHogFlags) refresh(distinctID string) {
	defer func() {
		p.mu.Lock()
		delete(p.inflight, distinctID)
		p.mu.Unlock()
	}()
	evaluated, err := p.fetch(distinctID)
	if err != nil {
		p.logger.Warn("posthog flag fetch failed", "distinct_id", distinctID, "err", err)
		return
	}
	p.mu.Lock()
	p.cache[distinctID] = evaluated
	p.mu.Unlock()
}

// fetch evaluates every flag for a distinct ID via POST /decide.
func (p *PostHogFlags) fetch(distinctID string) (flagSet, error) {
	body, _ := json.Marshal(map[string]string{"api_key": p.apiKey, "distinct_id": distinctID})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return flagSet{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.http.Do(req)
	if err != nil {
		return flagSet{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return flagSet{}, fmt.Errorf("posthog decide: status %s", resp.Status)
	}
	var decoded struct {
		FeatureFlags map[string]any `json:"featureFlags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return flagSet{}, err
	}
	fs := flagSet{
		flags:    make(map[string]bool, len(decoded.FeatureFlags)),
		variants: map[string]string{},
		fetched:  time.Now(),
	}
	for k, v := range decoded.FeatureFlags {
		// A flag is bool (on/off) or string (a multivariate variant). Any
		// variant counts as enabled and carries its variant name.
		switch t := v.(type) {
		case bool:
			fs.flags[k] = t
		case string:
			fs.flags[k] = t != "" && t != "false"
			fs.variants[k] = t
		}
	}
	return fs, nil
}
