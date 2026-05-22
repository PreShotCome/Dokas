// Package flags evaluates feature flags. Production reads flags from
// PostHog; without POSTHOG_API_KEY it falls back to StaticFlags, which reads
// FEATURE_<NAME> environment variables. Every new surface is meant to be
// gated through this interface.
package flags

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// Known flag keys. Keep them here so call sites and config docs agree.
const (
	// SelfServeSignup gates the public signup route. Off at GA — the
	// product is sales-led — so /signup shows a "contact sales" page.
	SelfServeSignup = "self_serve_signup"
)

// defaults are the values used when nothing overrides a flag. A flag absent
// from this map defaults to false.
var defaults = map[string]bool{
	SelfServeSignup: true, // open in dev; flip off for sales-led GA
}

// Flags evaluates a boolean flag for a distinct actor. distinctID lets a
// real provider do percentage rollouts / per-account targeting; StaticFlags
// ignores it.
type Flags interface {
	Enabled(ctx context.Context, key, distinctID string) bool
}

// StaticFlags resolves flags from FEATURE_<UPPER_SNAKE> env vars, falling
// back to the compiled defaults. Deterministic — the same answer for every
// actor — which is what dev, CI, and a simple prod want.
type StaticFlags struct{}

func NewStaticFlags() *StaticFlags { return &StaticFlags{} }

func (StaticFlags) Enabled(_ context.Context, key, _ string) bool {
	envName := "FEATURE_" + strings.ToUpper(key)
	if v, ok := os.LookupEnv(envName); ok {
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err == nil {
			return b
		}
	}
	return defaults[key]
}

// New is the constructor used at startup. With a PostHog project key it
// returns a PostHog-backed evaluator (which itself falls back to the static
// env defaults); without one, plain env-driven StaticFlags.
func New(posthogAPIKey, posthogHost string, logger *slog.Logger) Flags {
	if posthogAPIKey == "" {
		return NewStaticFlags()
	}
	return NewPostHogFlags(posthogAPIKey, posthogHost, logger)
}
