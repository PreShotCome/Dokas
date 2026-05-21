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

	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/auth"
	"github.com/preshotcome/anything/internal/config"
	"github.com/preshotcome/anything/internal/db"
	"github.com/preshotcome/anything/internal/drill"
	"github.com/preshotcome/anything/internal/drill/steps"
	"github.com/preshotcome/anything/internal/runner"
	"github.com/preshotcome/anything/internal/web/handlers"
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

	pool, err := db.Open(rootCtx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("db open", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	sessionStore := auth.NewStore(pool, cfg.IdleTimeout, cfg.AbsoluteMaxAge, cfg.IsProduction())
	auditLog := audit.New(pool)
	drillStore := drill.NewStore(pool)

	evidenceDir := cfg.EvidenceDir
	if err := os.MkdirAll(evidenceDir, 0o755); err != nil {
		logger.Error("evidence dir", "err", err)
		os.Exit(1)
	}

	localRunner := runner.NewLocalRunner(cfg.DatabaseURL)

	workers := river.NewWorkers()
	riverClient, err := newRiverClient(rootCtx, pool, workers)
	if err != nil {
		logger.Error("river client", "err", err)
		os.Exit(1)
	}

	steps.Register(workers, steps.Deps{
		Store:       drillStore,
		Runner:      localRunner,
		Inserter:    riverClient,
		Audit:       auditLog,
		EvidenceDir: evidenceDir,
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

	orch := drill.NewOrchestrator(drillStore, riverClient, auditLog)

	h := handlers.New(handlers.Deps{
		Pool:         pool,
		Sessions:     sessionStore,
		Audit:        auditLog,
		Drills:       drillStore,
		Orchestrator: orch,
	})

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

func newRiverClient(ctx context.Context, pool *pgxpool.Pool, workers *river.Workers) (*river.Client[pgx.Tx], error) {
	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 8},
		},
		Workers: workers,
	})
}
