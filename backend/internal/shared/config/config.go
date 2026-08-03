// Package config loads and validates application configuration from the environment.
package config

import (
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

	// AllowLocalEndpoints lets customers register http:// and private/loopback webhook URLs.
	// Development only: it disables SSRF protection, so production refuses to start with it on.
	AllowLocalEndpoints bool
}

const minAdminTokenLen = 16

func (c Config) IsProduction() bool { return c.Env == "production" }

// Load reads configuration from environment variables and validates it.
func Load() (Config, error) {
	cfg := Config{
		Env:         get("APP_ENV", "development"),
		HTTPAddr:    get("HTTP_ADDR", ":8080"),
		LogLevel:    get("LOG_LEVEL", "info"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		RedisURL:    os.Getenv("REDIS_URL"),
		AdminToken:  os.Getenv("ADMIN_TOKEN"),
	}

	var err error
	cfg.ShutdownTimeout, err = time.ParseDuration(get("SHUTDOWN_TIMEOUT", "15s"))
	if err != nil {
		return Config{}, fmt.Errorf("config: invalid SHUTDOWN_TIMEOUT: %w", err)
	}

	cfg.AllowLocalEndpoints, err = strconv.ParseBool(get("ALLOW_LOCAL_ENDPOINTS", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("config: invalid ALLOW_LOCAL_ENDPOINTS: %w", err)
	}
	if cfg.IsProduction() && cfg.AllowLocalEndpoints {
		return Config{}, fmt.Errorf("config: ALLOW_LOCAL_ENDPOINTS must not be enabled in production")
	}

	var missing []string
	if cfg.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if cfg.RedisURL == "" {
		missing = append(missing, "REDIS_URL")
	}
	if cfg.AdminToken == "" {
		missing = append(missing, "ADMIN_TOKEN")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("config: missing required env vars: %s", strings.Join(missing, ", "))
	}
	if len(cfg.AdminToken) < minAdminTokenLen {
		return Config{}, fmt.Errorf("config: ADMIN_TOKEN must be at least %d characters", minAdminTokenLen)
	}
	return cfg, nil
}

func get(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
