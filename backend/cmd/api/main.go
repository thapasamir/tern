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

	"github.com/go-chi/chi/v5"

	"github.com/samirthapa/tern/internal/features/delivery"
	"github.com/samirthapa/tern/internal/features/endpoint"
	"github.com/samirthapa/tern/internal/features/event"
	"github.com/samirthapa/tern/internal/features/tenant"
	"github.com/samirthapa/tern/internal/shared/cache"
	"github.com/samirthapa/tern/internal/shared/config"
	"github.com/samirthapa/tern/internal/shared/database"
	"github.com/samirthapa/tern/internal/shared/httpx"
	"github.com/samirthapa/tern/internal/shared/logger"
	"github.com/samirthapa/tern/internal/shared/queue"
	"github.com/samirthapa/tern/internal/shared/urlguard"
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

	r := chi.NewRouter()
	r.Use(httpx.RequestID, httpx.Logger(log), httpx.Recoverer(log))

	r.Get("/healthz", httpx.Health)
	r.Get("/readyz", httpx.Ready(map[string]httpx.Checker{
		"postgres": func(ctx context.Context) error { return pool.Ping(ctx) },
		"redis":    func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
	}))

	// Feature modules. Each exposes RegisterRoutes; main only mounts them.
	tenantMod := tenant.NewModule(pool, cfg.AdminToken)
	endpointMod := endpoint.NewModule(pool, urlguard.NewValidator(cfg.AllowLocalEndpoints))
	deliveryMod := delivery.NewModule(pool, queue.New(rdb, delivery.QueueName))
	eventMod := event.NewModule(pool, deliveryMod.Service()) // events enqueue deliveries through it
	if cfg.AllowLocalEndpoints {
		log.Warn("ALLOW_LOCAL_ENDPOINTS is on: SSRF protection is relaxed (development only)")
	}

	r.Route("/admin", func(r chi.Router) {
		r.Use(tenantMod.RequireAdmin)
		tenantMod.RegisterAdminRoutes(r)
	})
	// Every future feature (endpoints, events, deliveries) registers inside this group.
	r.Route("/v1", func(r chi.Router) {
		r.Use(tenantMod.Authenticate)
		tenantMod.RegisterRoutes(r)
		endpointMod.RegisterRoutes(r)
		eventMod.RegisterRoutes(r)
		deliveryMod.RegisterRoutes(r)
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           r,
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
