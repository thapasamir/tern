package config

import (
	"strings"
	"testing"
	"time"
)

// setEnv applies a complete, valid environment, then the overrides. An empty override unsets a variable.
func setEnv(t *testing.T, overrides map[string]string) {
	t.Helper()
	base := map[string]string{
		"APP_ENV":               "",
		"DATABASE_URL":          "postgres://x",
		"REDIS_URL":             "redis://x",
		"ADMIN_TOKEN":           "0123456789abcdef",
		"SHUTDOWN_TIMEOUT":      "",
		"ALLOW_LOCAL_ENDPOINTS": "",
		"WORKER_CONCURRENCY":    "",
		"DELIVERY_TIMEOUT":      "",
		"HTTP_ADDR":             "",
		"LOG_LEVEL":             "",
	}
	for k, v := range overrides {
		base[k] = v
	}
	for k, v := range base {
		t.Setenv(k, v) // t.Setenv with "" behaves as unset for our lookups (we treat "" as absent)
	}
}

func TestLoad_Defaults(t *testing.T) {
	setEnv(t, nil)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Env != "development" || cfg.HTTPAddr != ":8080" || cfg.ShutdownTimeout != 15*time.Second ||
		cfg.WorkerConcurrency != 10 || cfg.DeliveryTimeout != 10*time.Second || cfg.AllowLocalEndpoints {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoad_ReadsOverrides(t *testing.T) {
	setEnv(t, map[string]string{
		"WORKER_CONCURRENCY":    "25",
		"DELIVERY_TIMEOUT":      "30s",
		"ALLOW_LOCAL_ENDPOINTS": "true",
		"SHUTDOWN_TIMEOUT":      "1m",
	})
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WorkerConcurrency != 25 || cfg.DeliveryTimeout != 30*time.Second || !cfg.AllowLocalEndpoints || cfg.ShutdownTimeout != time.Minute {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

func TestLoad_Rejections(t *testing.T) {
	cases := map[string]struct {
		env  map[string]string
		want string
	}{
		"missing database":           {map[string]string{"DATABASE_URL": ""}, "DATABASE_URL"},
		"missing redis":              {map[string]string{"REDIS_URL": ""}, "REDIS_URL"},
		"missing admin token":        {map[string]string{"ADMIN_TOKEN": ""}, "ADMIN_TOKEN"},
		"short admin token":          {map[string]string{"ADMIN_TOKEN": "short"}, "at least"},
		"local endpoints in prod":    {map[string]string{"APP_ENV": "production", "ALLOW_LOCAL_ENDPOINTS": "true"}, "production"},
		"bad bool":                   {map[string]string{"ALLOW_LOCAL_ENDPOINTS": "maybe"}, "ALLOW_LOCAL_ENDPOINTS"},
		"bad duration":               {map[string]string{"SHUTDOWN_TIMEOUT": "soon"}, "SHUTDOWN_TIMEOUT"},
		"non-numeric concurrency":    {map[string]string{"WORKER_CONCURRENCY": "many"}, "WORKER_CONCURRENCY"},
		"zero concurrency":           {map[string]string{"WORKER_CONCURRENCY": "0"}, "WORKER_CONCURRENCY"},
		"excessive concurrency":      {map[string]string{"WORKER_CONCURRENCY": "100000"}, "WORKER_CONCURRENCY"},
		"delivery timeout too short": {map[string]string{"DELIVERY_TIMEOUT": "10ms"}, "DELIVERY_TIMEOUT"},
		"delivery timeout too long":  {map[string]string{"DELIVERY_TIMEOUT": "1h"}, "DELIVERY_TIMEOUT"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			setEnv(t, c.env)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Load error = %v, want one mentioning %q", err, c.want)
			}
		})
	}
}

func TestLoad_ProductionWithoutLocalEndpointsIsFine(t *testing.T) {
	setEnv(t, map[string]string{"APP_ENV": "production"})
	cfg, err := Load()
	if err != nil || !cfg.IsProduction() {
		t.Fatalf("Load = %+v, %v; want a valid production config", cfg, err)
	}
}
