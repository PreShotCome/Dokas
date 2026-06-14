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

	"github.com/preshotcome/dokaz/internal/account"
	"github.com/preshotcome/dokaz/internal/analytics"
	"github.com/preshotcome/dokaz/internal/apikey"
	"github.com/preshotcome/dokaz/internal/audit"
	"github.com/preshotcome/dokaz/internal/auth"
	"github.com/preshotcome/dokaz/internal/billing"
	"github.com/preshotcome/dokaz/internal/compliance"
	"github.com/preshotcome/dokaz/internal/config"
	"github.com/preshotcome/dokaz/internal/db"
	"github.com/preshotcome/dokaz/internal/drill"
	"github.com/preshotcome/dokaz/internal/drill/steps"
	"github.com/preshotcome/dokaz/internal/email"
	"github.com/preshotcome/dokaz/internal/evidence"
	"github.com/preshotcome/dokaz/internal/flags"
	"github.com/preshotcome/dokaz/internal/fly"
	"github.com/preshotcome/dokaz/internal/heartbeat"
	heartbeatnotify "github.com/preshotcome/dokaz/internal/heartbeat/notify"
	"github.com/preshotcome/dokaz/internal/mobileauth"
	"github.com/preshotcome/dokaz/internal/oauth"
	"github.com/preshotcome/dokaz/internal/obs"
	"github.com/preshotcome/dokaz/internal/push"
	"github.com/preshotcome/dokaz/internal/ratelimit"
	"github.com/preshotcome/dokaz/internal/runner"
	"github.com/preshotcome/dokaz/internal/web/csrf"
	"github.com/preshotcome/dokaz/internal/web/handlers"
	"github.com/preshotcome/dokaz/internal/webhooks"
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
	heartbeatStore := heartbeat.NewStore(pool)
	accountStore := account.NewStore(pool)
	billingCustomers := billing.New(billing.Config{
		SecretKey:     cfg.StripeSecretKey,
		WebhookSecret: cfg.StripeWebhookSecret,
		PriceStarter:  cfg.StripePriceStarter,
		PricePro:      cfg.StripePricePro,
		MeterEvent:    cfg.StripeMeterEvent,
	})
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

	var drillRunner runner.Runner = runner.NewLocalRunner(cfg.DatabaseURL)
	if cfg.FlyAPIToken != "" && cfg.FlyAppName != "" {
		drillRunner = runner.NewFlyMachineRunner(
			fly.NewClient(cfg.FlyAppName, cfg.FlyAPIToken),
			cfg.FlyAppName, cfg.FlyPostgresImage, cfg.FlyRegion, cfg.FlySandboxDBPassword)
		logger.Info("drill runner: fly machines", "app", cfg.FlyAppName)
	}
	webhookStore := webhooks.NewStore(pool)

	// Growth: transactional email, product analytics, feature flags.
	mailer := email.NewMailer(pool,
		email.NewSender(cfg.PostmarkToken, cfg.EmailFrom, logger), logger)
	if mailer.ProviderEnabled() {
		logger.Info("email enabled (postmark)")
	} else {
		logger.Info("email disabled (no POSTMARK_TOKEN) — using log mailer")
	}
	// Heartbeat + drill alerts fan out to email (an account's members) and
	// mobile push (registered devices). Email uses the same Mailer (LogMailer
	// in dev); push uses the FCM HTTP v1 transport when FIREBASE_SERVICE_ACCOUNT
	// is configured, else a LogSender so the pipeline is observable in dev.
	pushDevices := push.NewStore(pool)
	var pushSender push.Sender = push.LogSender{Logger: logger}
	if fcm, err := push.NewFCMSender(cfg.FirebaseServiceAccount, logger); err != nil {
		logger.Error("fcm sender init", "err", err)
		os.Exit(1)
	} else if fcm != nil {
		pushSender = fcm
		logger.Info("push enabled (fcm)")
	} else {
		logger.Info("push disabled (no FIREBASE_SERVICE_ACCOUNT) — using log sender")
	}
	heartbeatNotifier := heartbeat.MultiNotifier{
		heartbeatnotify.New(mailer, accountStore, cfg.BaseURL, logger),
		push.NewHeartbeatNotifier(pushDevices, pushSender, logger),
	}
	drillNotifier := push.NewDrillNotifier(pushDevices, pushSender, logger)
	analyticsClient := analytics.New(cfg.PostHogAPIKey, cfg.PostHogHost, logger)
	featureFlags := flags.New(cfg.PostHogAPIKey, cfg.PostHogHost, logger)
	oauthRegistry := oauth.NewRegistry(
		cfg.GoogleOAuthClientID, cfg.GoogleOAuthClientSecret,
		cfg.GitHubOAuthClientID, cfg.GitHubOAuthClientSecret)
	if names := oauthRegistry.Names(); len(names) > 0 {
		logger.Info("social login enabled", "providers", names)
	}

	// Evidence: detached-signature signer + local store.
	signer, err := evidence.NewSignerWithVerificationKeys(cfg.EvidenceSigningKey, cfg.EvidenceVerificationKeys)
	if err != nil {
		logger.Error("evidence signer", "err", err)
		os.Exit(1)
	}
	if signer.Ephemeral() {
		logger.Warn("evidence signing key not configured — using an EPHEMERAL key; " +
			"signatures will not verify across restarts. Set EVIDENCE_SIGNING_KEY in production.")
	}
	evidenceCipher, err := evidence.NewCipher(cfg.EvidenceEncryptionKey, pool)
	if err != nil {
		logger.Error("evidence cipher", "err", err)
		os.Exit(1)
	}
	if evidenceCipher.Ephemeral() {
		logger.Warn("evidence encryption key not configured — using an EPHEMERAL master key; " +
			"evidence will not decrypt across restarts. Set EVIDENCE_ENCRYPTION_KEY in production.")
	}
	var evidenceStore evidence.Store = evidence.NewLocalStore(evidenceDir)
	if cfg.EvidenceS3Bucket != "" {
		evidenceStore = evidence.NewS3Store(cfg.EvidenceS3Bucket, cfg.EvidenceS3Region,
			cfg.EvidenceS3Endpoint, cfg.EvidenceS3AccessKeyID, cfg.EvidenceS3SecretAccessKey)
		logger.Info("evidence store: s3-compatible bucket", "bucket", cfg.EvidenceS3Bucket)
	}
	evidenceService := evidence.NewService(evidenceStore, signer, evidenceCipher, pool)

	loginThrottle := auth.NewLoginThrottle(pool, 5, 15*time.Minute)
	sweeper := compliance.NewSweeper(pool, evidenceService, loginThrottle).
		WithPingPruner(heartbeatStore)
	purger := compliance.NewPurger(pool, evidenceService, auditLog, compliance.DefaultGracePeriod)

	workers := river.NewWorkers()
	riverClient, err := newRiverClient(pool, workers)
	if err != nil {
		logger.Error("river client", "err", err)
		os.Exit(1)
	}

	webhookDispatch := webhooks.NewDispatcher(webhookStore, riverClient)

	// The orchestrator is built here (before workers are registered) so the
	// drill scheduler worker can enqueue drills through it.
	orch := drill.NewOrchestrator(drillStore, riverClient, auditLog)

	steps.Register(workers, steps.Deps{
		Store:     drillStore,
		Runner:    drillRunner,
		Inserter:  riverClient,
		Audit:     auditLog,
		Evidence:  evidenceService,
		Webhooks:  webhookDispatch,
		Metrics:   observ.Metrics,
		Analytics: analyticsClient,
		Billing:   billingCustomers,
		Accounts:  accountStore,
		Notify:    drillNotifier,
	})
	deliverWorker := webhooks.NewDeliverWorker(webhookStore, cfg.IsProduction())
	deliverWorker.Metrics = observ.Metrics
	river.AddWorker(workers, deliverWorker)
	river.AddWorker(workers, &compliance.PurgeWorker{Purger: purger})
	river.AddWorker(workers, &compliance.RetentionWorker{Sweeper: sweeper, Logger: logger})
	river.AddWorker(workers, &drill.SchedulerWorker{Store: drillStore, Orch: orch, Logger: logger})
	river.AddWorker(workers, &heartbeat.SweeperWorker{
		Store:    heartbeatStore,
		Dispatch: webhookDispatch,
		Audit:    auditLog,
		Notify:   heartbeatNotifier,
		Logger:   logger,
	})

	if err := riverClient.Start(rootCtx); err != nil {
		logger.Error("river start", "err", err)
		os.Exit(1)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer stopCancel()
		_ = riverClient.Stop(stopCtx)
	}()

	// Perimeter: per-IP / per-account rate limiters.
	authLimiter := ratelimit.New(20, 10)  // 20/min, burst 10 — login/signup
	appLimiter := ratelimit.New(300, 100) // 300/min, burst 100 — authed traffic
	v1Limiter := ratelimit.New(60, 60)    // 60/min — /v1 API, per account
	pingLimiter := ratelimit.New(120, 60) // 120/min per IP — public ping ingest
	authLimiter.StartSweeper(rootCtx.Done(), 5*time.Minute, 15*time.Minute)
	appLimiter.StartSweeper(rootCtx.Done(), 5*time.Minute, 15*time.Minute)
	v1Limiter.StartSweeper(rootCtx.Done(), 5*time.Minute, 15*time.Minute)
	pingLimiter.StartSweeper(rootCtx.Done(), 5*time.Minute, 15*time.Minute)

	h := handlers.New(handlers.Deps{
		Pool:            pool,
		Sessions:        sessionStore,
		Audit:           auditLog,
		Drills:          drillStore,
		Orchestrator:    orch,
		Heartbeats:      heartbeatStore,
		HeartbeatNotify: heartbeatNotifier,
		PingLimiter:     pingLimiter,
		Accounts:        accountStore,
		Billing:         billingCustomers,
		Throttle:        loginThrottle,
		Webhooks:        webhookStore,
		WebhookDispatch: webhookDispatch,
		CSRF:            csrf.New(cfg.IsProduction(), "/webhooks/", "/v1/", "/ping/", "/mobile/"),
		AuthLimiter:     authLimiter,
		AppLimiter:      appLimiter,
		Evidence:        evidenceService,
		Signer:          signer,
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
		MobileTokens:         mobileauth.NewStore(pool),
		PushDevices:          pushDevices,
		V1Limiter:            v1Limiter,
		SourceDir:            cfg.SourceDir,
		OAuth:                oauthRegistry,
		SecureCookies:        cfg.IsProduction(),
		PriceStarterLabel:    cfg.PriceStarterLabel,
		PriceProLabel:        cfg.PriceProLabel,
		BaseURL:              cfg.BaseURL,
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
			drill.SchedulerPeriodicJob(),
			heartbeat.SweeperPeriodicJob(),
		},
	})
}
