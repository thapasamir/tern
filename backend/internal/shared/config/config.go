// Package config loads and validates application configuration from the environment.
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
	Env             string
	HTTPAddr        string
	LogLevel        string
	DatabaseURL     string
	RedisURL        string
	AdminToken      string
	ShutdownTimeout time.Duration

	// AllowLocalEndpoints lets customers register http:// and private/loopback webhook URLs, and
	// lets the worker deliver to them. Development only: it disables SSRF protection, so
	// production refuses to start with it on.
	AllowLocalEndpoints bool

	// WorkerConcurrency is how many deliveries one worker process attempts at the same time.
	WorkerConcurrency int
	// DeliveryTimeout bounds a single HTTP request to a customer's endpoint.
	DeliveryTimeout time.Duration
}

const (
	minAdminTokenLen   = 16
	maxWorkerCount     = 1000
	minDeliveryTimeout = time.Second
	maxDeliveryTimeout = 2 * time.Minute
)

func (c Config) IsProduction() bool { return c.Env == "production" }

// Load reads configuration from environment variables and validates it.
func Load() (Config, error) {
	cfg := Config{
		Env:         env("APP_ENV", "development"),
		HTTPAddr:    env("HTTP_ADDR", ":8080"),
		LogLevel:    env("LOG_LEVEL", "info"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		RedisURL:    os.Getenv("REDIS_URL"),
		AdminToken:  os.Getenv("ADMIN_TOKEN"),
	}

	var err error
	if cfg.ShutdownTimeout, err = envDuration("SHUTDOWN_TIMEOUT", 15*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.AllowLocalEndpoints, err = envBool("ALLOW_LOCAL_ENDPOINTS", false); err != nil {
		return Config{}, err
	}
	if cfg.WorkerConcurrency, err = envInt("WORKER_CONCURRENCY", 10); err != nil {
		return Config{}, err
	}
	if cfg.DeliveryTimeout, err = envDuration("DELIVERY_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	var missing []string
	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if c.RedisURL == "" {
		missing = append(missing, "REDIS_URL")
	}
	if c.AdminToken == "" {
		missing = append(missing, "ADMIN_TOKEN")
	}
	if len(missing) > 0 {
		return fmt.Errorf("config: missing required env vars: %s", strings.Join(missing, ", "))
	}

	switch {
	case len(c.AdminToken) < minAdminTokenLen:
		return fmt.Errorf("config: ADMIN_TOKEN must be at least %d characters", minAdminTokenLen)
	case c.IsProduction() && c.AllowLocalEndpoints:
		return errors.New("config: ALLOW_LOCAL_ENDPOINTS must not be enabled in production")
	case c.WorkerConcurrency < 1 || c.WorkerConcurrency > maxWorkerCount:
		return fmt.Errorf("config: WORKER_CONCURRENCY must be between 1 and %d", maxWorkerCount)
	case c.DeliveryTimeout < minDeliveryTimeout || c.DeliveryTimeout > maxDeliveryTimeout:
		return fmt.Errorf("config: DELIVERY_TIMEOUT must be between %s and %s", minDeliveryTimeout, maxDeliveryTimeout)
	}
	return nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) (bool, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("config: invalid %s: %w", key, err)
	}
	return v, nil
}

func envInt(key string, fallback int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("config: invalid %s: %w", key, err)
	}
	return v, nil
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("config: invalid %s: %w", key, err)
	}
	return v, nil
}
