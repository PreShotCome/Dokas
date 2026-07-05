package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/preshotcome/dokaz/internal/branding"
)

type Config struct {
	Addr        string
	DatabaseURL string
	SessionKey  []byte
	Environment string
	// BaseURL is the canonical public origin (scheme + host, no path) of
	// this app. Outbound URLs in emails and OAuth redirect URIs are built
	// from it — never from the request's Host or X-Forwarded-Proto, which
	// an upstream proxy may not pin. Required outside dev.
	BaseURL        string
	IdleTimeout    time.Duration
	AbsoluteMaxAge time.Duration
	EvidenceDir    string
	// SourceDir is the only directory a database target's source dump may
	// live under — customer-supplied paths are confined to it.
	SourceDir           string
	StripeSecretKey     string
	StripeWebhookSecret string
	StripePriceStarter  string // Starter — $100/mo
	StripePricePro      string // Growth (internal id "pro") — $300/mo
	StripePriceScale    string // Grounded (internal id "scale") — $600/mo
	// StripeMeterEvent is the Stripe Billing Meter event name drill usage is
	// reported under. Empty disables usage-based billing.
	StripeMeterEvent string
	// Price*Label values are the headline monthly prices shown on the public
	// /pricing page. They are display-only — the amount actually charged is
	// set on the Stripe Price — so they must be kept in sync with Stripe by
	// whoever configures the account.
	PriceStarterLabel  string
	PriceProLabel      string // labels the Growth tier
	PriceScaleLabel    string
	EvidenceSigningKey string
	// EvidenceVerificationKeys holds zero or more concatenated PEM public-key
	// blocks — keys retired by rotation, kept so old evidence still verifies.
	EvidenceVerificationKeys string
	// EvidenceEncryptionKey is the base64 32-byte master key that wraps each
	// account's evidence data-encryption key (at-rest encryption / crypto-shred).
	EvidenceEncryptionKey string
	// EvidenceS3* configure an S3-compatible evidence bucket; with an empty
	// bucket evidence is stored on local disk instead.
	EvidenceS3Bucket          string
	EvidenceS3Region          string
	EvidenceS3Endpoint        string
	EvidenceS3AccessKeyID     string
	EvidenceS3SecretAccessKey string
	SentryDSN                 string

	PostmarkToken        string
	PostmarkWebhookToken string
	EmailFrom            string
	PostHogAPIKey        string
	PostHogHost          string

	GoogleOAuthClientID     string
	GoogleOAuthClientSecret string
	GitHubOAuthClientID     string
	GitHubOAuthClientSecret string

	// Fly* configure the Fly Machines drill runner; with no token the local
	// runner (a temp database on the app's Postgres host) is used instead.
	FlyAPIToken          string
	FlyAppName           string
	FlyPostgresImage     string
	FlyRegion            string
	FlySandboxDBPassword string

	StaffEmails  []string
	MetricsToken string

	// FirebaseServiceAccount is the Firebase Cloud Messaging service-account
	// JSON used by the responder app's push channel. Either inline JSON or a
	// path to the JSON file. Empty disables real push — the LogSender is used
	// instead and registrations + dispatches are observable without Firebase.
	FirebaseServiceAccount string
}

func Load() (Config, error) {
	c := Config{
		Addr:                     getenv("ADDR", ":8080"),
		DatabaseURL:              os.Getenv("DATABASE_URL"),
		Environment:              getenv("ENV", "dev"),
		BaseURL:                  strings.TrimRight(os.Getenv("BASE_URL"), "/"),
		IdleTimeout:              14 * 24 * time.Hour,
		AbsoluteMaxAge:           30 * 24 * time.Hour,
		EvidenceDir:              getenv("EVIDENCE_DIR", "tmp/evidence"),
		SourceDir:                getenv("SOURCE_DIR", "tmp/sources"),
		StripeSecretKey:          os.Getenv("STRIPE_SECRET_KEY"),
		StripeWebhookSecret:      os.Getenv("STRIPE_WEBHOOK_SECRET"),
		StripePriceStarter:       os.Getenv("STRIPE_PRICE_STARTER"),
		StripePricePro:           os.Getenv("STRIPE_PRICE_PRO"),
		StripePriceScale:         os.Getenv("STRIPE_PRICE_SCALE"),
		StripeMeterEvent:         os.Getenv("STRIPE_METER_EVENT"),
		PriceStarterLabel:        getenv("PRICE_STARTER_LABEL", "$100"),
		PriceProLabel:            getenv("PRICE_PRO_LABEL", "$300"),
		PriceScaleLabel:          getenv("PRICE_SCALE_LABEL", "$600"),
		EvidenceSigningKey:       os.Getenv("EVIDENCE_SIGNING_KEY"),
		EvidenceVerificationKeys: os.Getenv("EVIDENCE_VERIFICATION_KEYS"),
		EvidenceEncryptionKey:    os.Getenv("EVIDENCE_ENCRYPTION_KEY"),

		EvidenceS3Bucket:          os.Getenv("EVIDENCE_S3_BUCKET"),
		EvidenceS3Region:          os.Getenv("EVIDENCE_S3_REGION"),
		EvidenceS3Endpoint:        os.Getenv("EVIDENCE_S3_ENDPOINT"),
		EvidenceS3AccessKeyID:     os.Getenv("EVIDENCE_S3_ACCESS_KEY_ID"),
		EvidenceS3SecretAccessKey: os.Getenv("EVIDENCE_S3_SECRET_ACCESS_KEY"),
		SentryDSN:                 os.Getenv("SENTRY_DSN"),

		PostmarkToken:        os.Getenv("POSTMARK_TOKEN"),
		PostmarkWebhookToken: os.Getenv("POSTMARK_WEBHOOK_TOKEN"),
		EmailFrom:            getenv("EMAIL_FROM", branding.EmailFrom),
		PostHogAPIKey:        os.Getenv("POSTHOG_API_KEY"),
		PostHogHost:          os.Getenv("POSTHOG_HOST"),

		GoogleOAuthClientID:     os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
		GoogleOAuthClientSecret: os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
		GitHubOAuthClientID:     os.Getenv("GITHUB_OAUTH_CLIENT_ID"),
		GitHubOAuthClientSecret: os.Getenv("GITHUB_OAUTH_CLIENT_SECRET"),

		FlyAPIToken:          os.Getenv("FLY_API_TOKEN"),
		FlyAppName:           os.Getenv("FLY_APP_NAME"),
		FlyPostgresImage:     os.Getenv("FLY_POSTGRES_IMAGE"),
		FlyRegion:            getenv("FLY_REGION", "iad"),
		FlySandboxDBPassword: os.Getenv("FLY_SANDBOX_DB_PASSWORD"),

		StaffEmails:  parseList(os.Getenv("STAFF_EMAILS")),
		MetricsToken: os.Getenv("METRICS_TOKEN"),

		FirebaseServiceAccount: os.Getenv("FIREBASE_SERVICE_ACCOUNT"),
	}

	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}

	// BASE_URL must be set in any non-dev environment — without it the
	// outbound URLs in emails and OAuth redirect URIs fall back to the
	// request's Host header, which an attacker can forge to exfiltrate
	// magic-link tokens.
	if c.Environment != "dev" && c.BaseURL == "" {
		return c, errors.New("BASE_URL is required outside dev (e.g. https://" + branding.DomainApp + ")")
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
