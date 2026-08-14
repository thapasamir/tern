package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/samirthapa/tern/internal/features/delivery"
	"github.com/samirthapa/tern/internal/features/delivery/dispatch"
	"github.com/samirthapa/tern/internal/features/delivery/retry"
	"github.com/samirthapa/tern/internal/features/delivery/sender"
	"github.com/samirthapa/tern/internal/features/endpoint"
	"github.com/samirthapa/tern/internal/features/retention"
	"github.com/samirthapa/tern/internal/shared/cache"
	"github.com/samirthapa/tern/internal/shared/config"
	"github.com/samirthapa/tern/internal/shared/database"
	"github.com/samirthapa/tern/internal/shared/logger"
	"github.com/samirthapa/tern/internal/shared/metrics"
	"github.com/samirthapa/tern/internal/shared/queue"
	"github.com/samirthapa/tern/internal/shared/ratelimit"
	"github.com/samirthapa/tern/internal/shared/urlguard"
	"github.com/samirthapa/tern/internal/worker"
)

const (
	pollInterval = 500 * time.Millisecond
	// dbMargin is what a job may use beyond the HTTP request itself, for its database reads and writes.
	dbMargin = 15 * time.Second

	recoverInterval = 10 * time.Second
	sweepInterval   = 15 * time.Second
	purgeInterval   = time.Hour
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

	// What the worker does is measured through observers; the metrics are served on their own port.
	reg := metrics.NewRegistry()
	workerMetrics := metrics.NewWorker(reg)

	jobs := queue.New(rdb, delivery.QueueName)
	repo := delivery.NewRepository(pool)
	deliveries := delivery.NewService(repo, jobs)
	// The endpoint feature owns an endpoint's health; the processor reports each attempt to it.
	endpoints := endpoint.NewService(endpoint.NewRepository(pool), urlguard.NewValidator(cfg.AllowLocalEndpoints))
	policy := retry.NewPolicy(cfg.RetrySchedule)
	// Each endpoint may only receive so many deliveries per second, whichever worker sends them.
	throttle := dispatch.NewThrottle(
		ratelimit.NewRedis(rdb, "tern:ratelimit"),
		ratelimit.PerSecond(float64(cfg.EndpointDeliveriesPerSecond)),
	)
	processor := dispatch.NewProcessor(
		repo,
		sender.New(cfg.AllowLocalEndpoints, cfg.DeliveryTimeout),
		endpoints,
		dispatch.ProcessorConfig{
			Retry:            policy,
			AutoDisableAfter: cfg.AutoDisableAfter,
			Throttle:         throttle,
			Observer:         workerMetrics,
		},
	)
	if cfg.AllowLocalEndpoints {
		log.Warn("ALLOW_LOCAL_ENDPOINTS is on: deliveries to private addresses are allowed (development only)")
	}

	jobTimeout := cfg.DeliveryTimeout + dbMargin
	runner, err := worker.NewRunner(jobs, processor, worker.Options{
		Concurrency:  cfg.WorkerConcurrency,
		PollInterval: pollInterval,
		JobTimeout:   jobTimeout,
		Lease:        3 * jobTimeout, // comfortably longer than a job can run
		Observer:     workerMetrics,
	}, log)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup

	log.Info("worker started",
		"concurrency", cfg.WorkerConcurrency,
		"delivery_timeout", cfg.DeliveryTimeout,
		"max_attempts", policy.MaxAttempts(),
		"auto_disable_after", cfg.AutoDisableAfter,
		"endpoint_deliveries_per_second", cfg.EndpointDeliveriesPerSecond,
	)
	wg.Go(func() { runner.Run(ctx) })

	// Jobs whose worker died hold a lease that eventually expires; hand them back to the queue.
	wg.Go(func() {
		worker.Every(ctx, log, recoverInterval, "recover expired leases", func(ctx context.Context) error {
			n, err := jobs.RecoverExpired(ctx)
			if n > 0 {
				log.Warn("recovered jobs whose worker did not finish them", "count", n)
			}
			return err
		})
	})
	// Deliveries committed to Postgres but never queued (crash or Redis outage in between).
	wg.Go(func() {
		worker.Every(ctx, log, sweepInterval, "enqueue orphaned deliveries", deliveries.EnqueueOrphans)
	})
	// Deliveries whose queue job vanished, e.g. because Redis lost its data: Postgres knows
	// when each is due, so the queue can be rebuilt from it.
	wg.Go(func() {
		worker.Every(ctx, log, cfg.ReconcileInterval, "reconcile overdue deliveries", deliveries.ReconcileOverdue)
	})
	// Old events, with their deliveries and attempts, are deleted once past the retention period.
	purger := retention.NewService(retention.NewRepository(pool), time.Duration(cfg.RetentionDays)*24*time.Hour)
	wg.Go(func() {
		worker.Every(ctx, log, purgeInterval, "purge old events", func(ctx context.Context) error {
			_, err := purger.Purge(ctx)
			return err
		})
	})

	// The worker has no API, so it serves its metrics (and a liveness check) on a separate port.
	var metricsServer *http.Server
	if cfg.WorkerMetricsAddr != "" {
		metricsServer = reg.Server(cfg.WorkerMetricsAddr)
		wg.Go(func() {
			log.Info("worker metrics listening", "addr", cfg.WorkerMetricsAddr)
			if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("worker metrics server failed", "error", err)
			}
		})
	}

	<-ctx.Done()
	log.Info("shutdown signal received; waiting for running deliveries to finish")
	if metricsServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		_ = metricsServer.Shutdown(shutdownCtx)
	}
	wg.Wait()
	log.Info("worker stopped cleanly")
	return nil
}
