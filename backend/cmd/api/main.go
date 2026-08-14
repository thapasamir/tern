package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/samirthapa/tern/internal/api"
	"github.com/samirthapa/tern/internal/features/delivery"
	"github.com/samirthapa/tern/internal/shared/cache"
	"github.com/samirthapa/tern/internal/shared/config"
	"github.com/samirthapa/tern/internal/shared/database"
	"github.com/samirthapa/tern/internal/shared/logger"
	"github.com/samirthapa/tern/internal/shared/metrics"
	"github.com/samirthapa/tern/internal/shared/queue"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logger.New(cfg.LogLevel, cfg.IsProduction())
	slog.SetDefault(log)

	// Cancelled on SIGINT/SIGTERM; drives graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	rdb, err := cache.Connect(ctx, cfg.RedisURL)
	if err != nil {
		return err
	}
	defer rdb.Close()

	// Metrics. The queue's depth is read from Redis on every scrape, so it is current whichever
	// process produced or consumes the jobs.
	reg := metrics.NewRegistry()
	reg.RegisterQueueDepth(queueDepth(queue.New(rdb, delivery.QueueName)), log)
	httpMetrics := metrics.NewHTTP(reg)

	if cfg.AllowLocalEndpoints {
		log.Warn("ALLOW_LOCAL_ENDPOINTS is on: SSRF protection is relaxed (development only)")
	}
	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: api.NewRouter(api.Deps{
			Pool:              pool,
			Redis:             rdb,
			Config:            cfg,
			Log:               log,
			HTTPObserver:      httpMetrics,
			RateLimitObserver: httpMetrics,
			MetricsHandler:    reg.Handler(),
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Info("api listening", "addr", cfg.HTTPAddr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	select {
	case err := <-serveErr:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	log.Info("api stopped cleanly")
	return nil
}

// queueDepth adapts the queue's depth report to the shape the metrics package exposes.
func queueDepth(q *queue.Redis) func(context.Context) (metrics.QueueDepth, error) {
	return func(ctx context.Context) (metrics.QueueDepth, error) {
		d, err := q.Depth(ctx)
		return metrics.QueueDepth{
			Due: d.Due, Scheduled: d.Scheduled, Processing: d.Processing, OldestDueAge: d.OldestDueAge,
		}, err
	}
}
