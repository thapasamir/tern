// Package config loads and validates application configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env         string
	HTTPAddr    string
	LogLevel    string
	DatabaseURL string
	RedisURL    string
	AdminToken  string
	// SecretsKey is the base64 AES-256 key that encrypts endpoint signing secrets at rest.
	SecretsKey      string
	ShutdownTimeout time.Duration

	// AllowLocalEndpoints lets customers register http:// and private/loopback webhook URLs, and
	// lets the worker deliver to them. Development only: it disables SSRF protection, so
	// production refuses to start with it on.
	AllowLocalEndpoints bool

	// WorkerConcurrency is how many deliveries one worker process attempts at the same time.
	WorkerConcurrency int
	// DeliveryTimeout bounds a single HTTP request to a customer's endpoint.
	DeliveryTimeout time.Duration

	// RetrySchedule is the delay after each failed delivery attempt: the first entry follows the
	// first failure, the second the second, and so on. A delivery gets len(RetrySchedule)+1
	// attempts in all, then fails for good. Empty means no retries.
	RetrySchedule []time.Duration
	// AutoDisableAfter is how many failed attempts in a row disable an endpoint.
	AutoDisableAfter int
	// ReconcileInterval is how often the worker looks for deliveries whose queue job was lost.
	ReconcileInterval time.Duration

	// EventsPerSecond limits how many events each tenant may send per second; bursts of twice
	// that are allowed. Zero means unlimited.
	EventsPerSecond int
	// AuthAttemptsPerMinute limits how often one client address may sign up or in, which makes
	// guessing passwords slow. Zero means unlimited.
	AuthAttemptsPerMinute int
	// EndpointDeliveriesPerSecond limits how many deliveries per second are sent to any single
	// endpoint, which protects the customer's server from being flooded. Zero means unlimited.
	EndpointDeliveriesPerSecond int

	// RetentionDays is how long events, with their deliveries and attempts, are kept before the
	// worker deletes them. Zero keeps everything.
	RetentionDays int

	// WorkerMetricsAddr is where the worker serves /metrics. The port is not authenticated, so it
	// belongs on an internal network. Empty disables it.
	WorkerMetricsAddr string
}

const (
	minAdminTokenLen   = 16
	maxWorkerCount     = 1000
	minDeliveryTimeout = time.Second
	maxDeliveryTimeout = 2 * time.Minute

	maxRetries            = 20
	maxRetryDelay         = 7 * 24 * time.Hour
	maxAutoDisableAfter   = 10_000
	minReconcileInterval  = time.Second
	maxReconcileInterval  = time.Hour
	noRetriesValue        = "none"
	defaultAutoDisable    = 20
	defaultReconcileEvery = 5 * time.Minute

	maxRatePerSecond                   = 100_000
	defaultEventsPerSecond             = 100
	defaultAuthAttemptsPerMinute       = 20
	defaultEndpointDeliveriesPerSecond = 25

	defaultRetentionDays = 30
	maxRetentionDays     = 3650

	defaultWorkerMetricsAddr = ":9102"
)

// defaultRetrySchedule gives a delivery 8 attempts over roughly 28 hours, which rides out most
// outages: a quick retry for blips, then progressively longer waits for real downtime.
var defaultRetrySchedule = []time.Duration{
	5 * time.Second, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 5 * time.Hour, 10 * time.Hour, 10 * time.Hour,
}

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
		SecretsKey:  os.Getenv("SECRETS_ENCRYPTION_KEY"),
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
	if cfg.RetrySchedule, err = envDurationList("RETRY_SCHEDULE", defaultRetrySchedule); err != nil {
		return Config{}, err
	}
	if cfg.AutoDisableAfter, err = envInt("AUTO_DISABLE_AFTER", defaultAutoDisable); err != nil {
		return Config{}, err
	}
	if cfg.ReconcileInterval, err = envDuration("RECONCILE_INTERVAL", defaultReconcileEvery); err != nil {
		return Config{}, err
	}
	if cfg.EventsPerSecond, err = envInt("RATE_LIMIT_EVENTS_PER_SECOND", defaultEventsPerSecond); err != nil {
		return Config{}, err
	}
	if cfg.AuthAttemptsPerMinute, err = envInt("RATE_LIMIT_AUTH_PER_MINUTE", defaultAuthAttemptsPerMinute); err != nil {
		return Config{}, err
	}
	if cfg.EndpointDeliveriesPerSecond, err = envInt("RATE_LIMIT_ENDPOINT_DELIVERIES_PER_SECOND", defaultEndpointDeliveriesPerSecond); err != nil {
		return Config{}, err
	}
	if cfg.RetentionDays, err = envInt("RETENTION_DAYS", defaultRetentionDays); err != nil {
		return Config{}, err
	}
	cfg.WorkerMetricsAddr = envPresent("WORKER_METRICS_ADDR", defaultWorkerMetricsAddr)

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
	if c.SecretsKey == "" {
		missing = append(missing, "SECRETS_ENCRYPTION_KEY")
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
	case len(c.RetrySchedule) > maxRetries:
		return fmt.Errorf("config: RETRY_SCHEDULE may list at most %d delays", maxRetries)
	case c.AutoDisableAfter < 1 || c.AutoDisableAfter > maxAutoDisableAfter:
		return fmt.Errorf("config: AUTO_DISABLE_AFTER must be between 1 and %d", maxAutoDisableAfter)
	case c.ReconcileInterval < minReconcileInterval || c.ReconcileInterval > maxReconcileInterval:
		return fmt.Errorf("config: RECONCILE_INTERVAL must be between %s and %s", minReconcileInterval, maxReconcileInterval)
	case c.EventsPerSecond < 0 || c.EventsPerSecond > maxRatePerSecond:
		return fmt.Errorf("config: RATE_LIMIT_EVENTS_PER_SECOND must be between 0 (unlimited) and %d", maxRatePerSecond)
	case c.RetentionDays < 0 || c.RetentionDays > maxRetentionDays:
		return fmt.Errorf("config: RETENTION_DAYS must be between 0 (keep everything) and %d", maxRetentionDays)
	case c.AuthAttemptsPerMinute < 0 || c.AuthAttemptsPerMinute > maxRatePerSecond:
		return fmt.Errorf("config: RATE_LIMIT_AUTH_PER_MINUTE must be between 0 (unlimited) and %d", maxRatePerSecond)
	case c.EndpointDeliveriesPerSecond < 0 || c.EndpointDeliveriesPerSecond > maxRatePerSecond:
		return fmt.Errorf("config: RATE_LIMIT_ENDPOINT_DELIVERIES_PER_SECOND must be between 0 (unlimited) and %d", maxRatePerSecond)
	}
	for _, d := range c.RetrySchedule {
		if d <= 0 || d > maxRetryDelay {
			return fmt.Errorf("config: every RETRY_SCHEDULE delay must be between 0 and %s", maxRetryDelay)
		}
	}
	return nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envPresent is like env, except that a variable that is set to the empty string stays empty.
// It is for settings where "empty" is a real choice, such as switching a listener off.
func envPresent(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
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

// envDurationList reads a comma-separated list of durations such as "5s,5m,1h". The value "none"
// means an empty list; an unset variable means fallback.
func envDurationList(key string, fallback []time.Duration) ([]time.Duration, error) {
	raw := os.Getenv(key)
	switch raw {
	case "":
		return slices.Clone(fallback), nil
	case noRetriesValue:
		return nil, nil
	}
	var out []time.Duration
	for _, part := range strings.Split(raw, ",") {
		d, err := time.ParseDuration(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("config: invalid %s: %w", key, err)
		}
		out = append(out, d)
	}
	return out, nil
}
