// Package config loads and validates application configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	Env             string
	HTTPAddr        string
	LogLevel        string
	DatabaseURL     string
	RedisURL        string
	ShutdownTimeout time.Duration
}

func (c Config) IsProduction() bool { return c.Env == "production" }

// Load reads configuration from environment variables and validates it.
func Load() (Config, error) {
	cfg := Config{
		Env:         get("APP_ENV", "development"),
		HTTPAddr:    get("HTTP_ADDR", ":8080"),
		LogLevel:    get("LOG_LEVEL", "info"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		RedisURL:    os.Getenv("REDIS_URL"),
	}

	var err error
	cfg.ShutdownTimeout, err = time.ParseDuration(get("SHUTDOWN_TIMEOUT", "15s"))
	if err != nil {
		return Config{}, fmt.Errorf("config: invalid SHUTDOWN_TIMEOUT: %w", err)
	}

	var missing []string
	if cfg.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if cfg.RedisURL == "" {
		missing = append(missing, "REDIS_URL")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("config: missing required env vars: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

func get(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
