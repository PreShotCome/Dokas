package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr               string
	DatabaseURL        string
	SessionKey         []byte
	Environment        string
	IdleTimeout        time.Duration
	AbsoluteMaxAge     time.Duration
	EvidenceDir        string
	StripeSecretKey    string
	EvidenceSigningKey string
	SentryDSN          string

	PostmarkToken        string
	PostmarkWebhookToken string
	EmailFrom            string
	PostHogAPIKey        string
	PostHogHost          string

	StaffEmails []string
}

func Load() (Config, error) {
	c := Config{
		Addr:               getenv("ADDR", ":8080"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		Environment:        getenv("ENV", "dev"),
		IdleTimeout:        14 * 24 * time.Hour,
		AbsoluteMaxAge:     30 * 24 * time.Hour,
		EvidenceDir:        getenv("EVIDENCE_DIR", "tmp/evidence"),
		StripeSecretKey:    os.Getenv("STRIPE_SECRET_KEY"),
		EvidenceSigningKey: os.Getenv("EVIDENCE_SIGNING_KEY"),
		SentryDSN:          os.Getenv("SENTRY_DSN"),

		PostmarkToken:        os.Getenv("POSTMARK_TOKEN"),
		PostmarkWebhookToken: os.Getenv("POSTMARK_WEBHOOK_TOKEN"),
		EmailFrom:            getenv("EMAIL_FROM", "notifications@restoredrill.io"),
		PostHogAPIKey:        os.Getenv("POSTHOG_API_KEY"),
		PostHogHost:          os.Getenv("POSTHOG_HOST"),
		StaffEmails:          parseList(os.Getenv("STAFF_EMAILS")),
	}

	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}

	key := os.Getenv("SESSION_KEY")
	if key == "" {
		if c.Environment != "dev" {
			return c, errors.New("SESSION_KEY is required outside dev")
		}
		key = "dev-only-do-not-use-in-production-please-rotate"
	}
	c.SessionKey = []byte(key)

	if v := os.Getenv("SESSION_IDLE_HOURS"); v != "" {
		hours, err := strconv.Atoi(v)
		if err != nil {
			return c, fmt.Errorf("invalid SESSION_IDLE_HOURS: %w", err)
		}
		c.IdleTimeout = time.Duration(hours) * time.Hour
	}

	return c, nil
}

func (c Config) IsProduction() bool {
	return c.Environment == "prod" || c.Environment == "production"
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// parseList splits a comma-separated env value into trimmed, lower-cased,
// non-empty entries.
func parseList(v string) []string {
	if v == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(strings.ToLower(part)); p != "" {
			out = append(out, p)
		}
	}
	return out
}
