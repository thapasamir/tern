package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/samirthapa/webhook-platform/internal/features/delivery"
	"github.com/samirthapa/webhook-platform/internal/shared/cache"
	"github.com/samirthapa/webhook-platform/internal/shared/config"
	"github.com/samirthapa/webhook-platform/internal/shared/database"
	"github.com/samirthapa/webhook-platform/internal/shared/logger"
	"github.com/samirthapa/webhook-platform/internal/shared/queue"
	"github.com/samirthapa/webhook-platform/internal/worker"
)

const (
	pollInterval = 500 * time.Millisecond
	// dbMargin is what a job may use beyond the HTTP request itself, for its database reads and writes.
	dbMargin = 15 * time.Second

	recoverInterval = 10 * time.Second
	sweepInterval   = 15 * time.Second
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

	jobs := queue.New(rdb, delivery.QueueName)
	repo := delivery.NewRepository(pool)
	deliveries := delivery.NewService(repo, jobs)
	processor := delivery.NewProcessor(repo, delivery.NewHTTPSender(cfg.AllowLocalEndpoints, cfg.DeliveryTimeout))
	if cfg.AllowLocalEndpoints {
		log.Warn("ALLOW_LOCAL_ENDPOINTS is on: deliveries to private addresses are allowed (development only)")
	}

	jobTimeout := cfg.DeliveryTimeout + dbMargin
	runner, err := worker.NewRunner(jobs, processor, worker.Options{
		Concurrency:  cfg.WorkerConcurrency,
		PollInterval: pollInterval,
		JobTimeout:   jobTimeout,
		Lease:        3 * jobTimeout, // comfortably longer than a job can run
	}, log)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	spawn := func(fn func()) {
		wg.Add(1)
		go func() { defer wg.Done(); fn() }()
	}

	log.Info("worker started", "concurrency", cfg.WorkerConcurrency, "delivery_timeout", cfg.DeliveryTimeout)
	spawn(func() { runner.Run(ctx) })

	// Jobs whose worker died hold a lease that eventually expires; hand them back to the queue.
	spawn(func() {
		worker.Every(ctx, log, recoverInterval, "recover expired leases", func(ctx context.Context) error {
			n, err := jobs.RecoverExpired(ctx)
			if n > 0 {
				log.Warn("recovered jobs whose worker did not finish them", "count", n)
			}
			return err
		})
	})
	// Deliveries committed to Postgres but never queued (crash or Redis outage in between).
	spawn(func() {
		worker.Every(ctx, log, sweepInterval, "enqueue orphaned deliveries", deliveries.EnqueueOrphans)
	})

	<-ctx.Done()
	log.Info("shutdown signal received; waiting for running deliveries to finish")
	wg.Wait()
	log.Info("worker stopped cleanly")
	return nil
}
