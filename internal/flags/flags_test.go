package flags

import (
	"context"
	"testing"
)

func TestStaticFlagsDefault(t *testing.T) {
	f := NewStaticFlags()
	ctx := context.Background()

	// self_serve_signup defaults to true (open in dev).
	if !f.Enabled(ctx, SelfServeSignup, "anyone") {
		t.Error("self_serve_signup should default to true")
	}
	// An unknown flag defaults to false.
	if f.Enabled(ctx, "no_such_flag", "anyone") {
		t.Error("unknown flag should default to false")
	}
}

func TestStaticFlagsEnvOverride(t *testing.T) {
	f := NewStaticFlags()
	ctx := context.Background()

	t.Setenv("FEATURE_SELF_SERVE_SIGNUP", "false")
	if f.Enabled(ctx, SelfServeSignup, "anyone") {
		t.Error("FEATURE_SELF_SERVE_SIGNUP=false should disable the flag")
	}

	t.Setenv("FEATURE_SELF_SERVE_SIGNUP", "true")
	if !f.Enabled(ctx, SelfServeSignup, "anyone") {
		t.Error("FEATURE_SELF_SERVE_SIGNUP=true should enable the flag")
	}

	// An override can also turn on a flag that defaults off.
	t.Setenv("FEATURE_NO_SUCH_FLAG", "true")
	if !f.Enabled(ctx, "no_such_flag", "anyone") {
		t.Error("an env override should be able to enable an unknown flag")
	}
}

func TestStaticFlagsIgnoresJunkEnv(t *testing.T) {
	f := NewStaticFlags()
	t.Setenv("FEATURE_SELF_SERVE_SIGNUP", "not-a-bool")
	// Unparseable value → fall back to the compiled default (true).
	if !f.Enabled(context.Background(), SelfServeSignup, "anyone") {
		t.Error("junk env value should fall back to the default")
	}
}
