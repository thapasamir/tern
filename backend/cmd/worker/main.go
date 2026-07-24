package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/samirthapa/webhook-platform/internal/shared/cache"
	"github.com/samirthapa/webhook-platform/internal/shared/config"
	"github.com/samirthapa/webhook-platform/internal/shared/database"
	"github.com/samirthapa/webhook-platform/internal/shared/logger"
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

	log.Info("worker started (no jobs yet; delivery loop arrives in phase 3)")
	<-ctx.Done()
	log.Info("worker stopped cleanly")
	return nil
}
