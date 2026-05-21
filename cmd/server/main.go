package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/preshotcome/anything/internal/account"
	"github.com/preshotcome/anything/internal/analytics"
	"github.com/preshotcome/anything/internal/apikey"
	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/auth"
	"github.com/preshotcome/anything/internal/billing"
	"github.com/preshotcome/anything/internal/compliance"
	"github.com/preshotcome/anything/internal/config"
	"github.com/preshotcome/anything/internal/db"
	"github.com/preshotcome/anything/internal/drill"
	"github.com/preshotcome/anything/internal/drill/steps"
	"github.com/preshotcome/anything/internal/email"
	"github.com/preshotcome/anything/internal/evidence"
	"github.com/preshotcome/anything/internal/flags"
	"github.com/preshotcome/anything/internal/obs"
	"github.com/preshotcome/anything/internal/ratelimit"
	"github.com/preshotcome/anything/internal/runner"
	"github.com/preshotcome/anything/internal/web/csrf"
	"github.com/preshotcome/anything/internal/web/handlers"
	"github.com/preshotcome/anything/internal/webhooks"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config load", "err", err)
		os.Exit(1)
	}

	rootCtx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	observ, err := obs.Setup(rootCtx, obs.Config{
		Environment: cfg.Environment,
		SentryDSN:   cfg.SentryDSN,
	}, logger)
	if err != nil {
		logger.Error("obs setup", "err", err)
		os.Exit(1)
	}
	defer func() {
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer shutCancel()
		observ.Shutdown(shutCtx)
	}()

	pool, err := db.Open(rootCtx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("db open", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	sessionStore := auth.NewStore(pool, cfg.IdleTimeout, cfg.AbsoluteMaxAge, cfg.IsProduction())
	auditLog := audit.New(pool)
	drillStore := drill.NewStore(pool)
	accountStore := account.NewStore(pool)
	billingCustomers := billing.NewStripeCustomers(cfg.StripeSecretKey)
	if billingCustomers.Enabled() {
		logger.Info("billing enabled (stripe)")
	} else {
		logger.Info("billing disabled (no STRIPE_SECRET_KEY) — using noop")
	}

	evidenceDir := cfg.EvidenceDir
	if err := os.MkdirAll(evidenceDir, 0o755); err != nil {
		logger.Error("evidence dir", "err", err)
		os.Exit(1)
	}

	localRunner := runner.NewLocalRunner(cfg.DatabaseURL)
	webhookStore := webhooks.NewStore(pool)

	// Growth: transactional email, product analytics, feature flags.
	mailer := email.NewMailer(pool,
		email.NewSender(cfg.PostmarkToken, cfg.EmailFrom, logger), logger)
	if mailer.ProviderEnabled() {
		logger.Info("email enabled (postmark)")
	} else {
		logger.Info("email disabled (no POSTMARK_TOKEN) — using log mailer")
	}
	analyticsClient := analytics.New(cfg.PostHogAPIKey, cfg.PostHogHost, logger)
	featureFlags := flags.New()

	// Evidence: detached-signature signer + local store.
	signer, err := evidence.NewSigner(cfg.EvidenceSigningKey)
	if err != nil {
		logger.Error("evidence signer", "err", err)
		os.Exit(1)
	}
	if signer.Ephemeral() {
		logger.Warn("evidence signing key not configured — using an EPHEMERAL key; " +
			"signatures will not verify across restarts. Set EVIDENCE_SIGNING_KEY in production.")
	}
	evidenceService := evidence.NewService(evidence.NewLocalStore(evidenceDir), signer, pool)

	loginThrottle := auth.NewLoginThrottle(pool, 5, 15*time.Minute)
	sweeper := compliance.NewSweeper(pool, evidenceService, loginThrottle)
	purger := compliance.NewPurger(pool, evidenceService, auditLog, compliance.DefaultGracePeriod)

	workers := river.NewWorkers()
	riverClient, err := newRiverClient(pool, workers)
	if err != nil {
		logger.Error("river client", "err", err)
		os.Exit(1)
	}

	webhookDispatch := webhooks.NewDispatcher(webhookStore, riverClient)

	steps.Register(workers, steps.Deps{
		Store:     drillStore,
		Runner:    localRunner,
		Inserter:  riverClient,
		Audit:     auditLog,
		Evidence:  evidenceService,
		Webhooks:  webhookDispatch,
		Metrics:   observ.Metrics,
		Analytics: analyticsClient,
	})
	deliverWorker := webhooks.NewDeliverWorker(webhookStore, cfg.IsProduction())
	deliverWorker.Metrics = observ.Metrics
	river.AddWorker(workers, deliverWorker)
	river.AddWorker(workers, &compliance.PurgeWorker{Purger: purger})
	river.AddWorker(workers, &compliance.RetentionWorker{Sweeper: sweeper, Logger: logger})

	if err := riverClient.Start(rootCtx); err != nil {
		logger.Error("river start", "err", err)
		os.Exit(1)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer stopCancel()
		_ = riverClient.Stop(stopCtx)
	}()

	orch := drill.NewOrchestrator(drillStore, riverClient, auditLog)

	// Perimeter: per-IP / per-account rate limiters.
	authLimiter := ratelimit.New(20, 10)  // 20/min, burst 10 — login/signup
	appLimiter := ratelimit.New(300, 100) // 300/min, burst 100 — authed traffic
	v1Limiter := ratelimit.New(60, 60)    // 60/min — /v1 API, per account
	authLimiter.StartSweeper(rootCtx.Done(), 5*time.Minute, 15*time.Minute)
	appLimiter.StartSweeper(rootCtx.Done(), 5*time.Minute, 15*time.Minute)
	v1Limiter.StartSweeper(rootCtx.Done(), 5*time.Minute, 15*time.Minute)

	h := handlers.New(handlers.Deps{
		Pool:            pool,
		Sessions:        sessionStore,
		Audit:           auditLog,
		Drills:          drillStore,
		Orchestrator:    orch,
		Accounts:        accountStore,
		Billing:         billingCustomers,
		Throttle:        loginThrottle,
		Webhooks:        webhookStore,
		WebhookDispatch: webhookDispatch,
		CSRF:            csrf.New(cfg.IsProduction(), "/webhooks/", "/v1/"),
		AuthLimiter:     authLimiter,
		AppLimiter:      appLimiter,
		Evidence:        evidenceService,
		Exporter:        compliance.NewExporter(pool),
		Purger:          purger,
		Inserter:        riverClient,
		Obs:             observ,

		Mailer:               mailer,
		Analytics:            analyticsClient,
		Flags:                featureFlags,
		PostmarkWebhookToken: cfg.PostmarkWebhookToken,
		StaffEmails:          cfg.StaffEmails,
		MetricsToken:         cfg.MetricsToken,
		APIKeys:              apikey.NewStore(pool),
		V1Limiter:            v1Limiter,
	})

	// Sample River queue depth into the metrics gauge every 15s.
	go sampleQueueDepth(rootCtx, pool, observ.Metrics, logger)

	staticDir, _ := filepath.Abs("assets/static")

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           h.Router(http.Dir(staticDir)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	go func() {
		logger.Info("listening", "addr", cfg.Addr, "env", cfg.Environment, "evidence_dir", evidenceDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen", "err", err)
			cancel()
		}
	}()

	<-rootCtx.Done()
	logger.Info("shutdown initiated")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown", "err", err)
	}
	logger.Info("shutdown complete")
}

// sampleQueueDepth periodically counts available River jobs into the
// queue-depth gauge so a backlog is visible on the metrics dashboard.
func sampleQueueDepth(ctx context.Context, pool *pgxpool.Pool, m *obs.Metrics, logger *slog.Logger) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			var n int
			if err := pool.QueryRow(ctx, `
				SELECT count(*) FROM river_job WHERE state = 'available'
			`).Scan(&n); err != nil {
				logger.Warn("queue depth sample failed", "err", err)
				continue
			}
			m.SetQueueDepth(n)
		}
	}
}

func newRiverClient(pool *pgxpool.Pool, workers *river.Workers) (*river.Client[pgx.Tx], error) {
	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 8},
		},
		Workers: workers,
		PeriodicJobs: []*river.PeriodicJob{
			compliance.PeriodicJob(),
		},
	})
}
