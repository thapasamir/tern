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
		"RETRY_SCHEDULE":        "",
		"AUTO_DISABLE_AFTER":    "",
		"RECONCILE_INTERVAL":    "",
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
	if cfg.AutoDisableAfter != 20 || cfg.ReconcileInterval != 5*time.Minute {
		t.Fatalf("unexpected reliability defaults: %+v", cfg)
	}
	if len(cfg.RetrySchedule) != 7 || cfg.RetrySchedule[0] != 5*time.Second || cfg.RetrySchedule[6] != 10*time.Hour {
		t.Fatalf("default retry schedule = %v, want 7 delays from 5s to 10h (8 attempts)", cfg.RetrySchedule)
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
		"garbage in the schedule":    {map[string]string{"RETRY_SCHEDULE": "5s,soon"}, "RETRY_SCHEDULE"},
		"zero delay in the schedule": {map[string]string{"RETRY_SCHEDULE": "5s,0s"}, "RETRY_SCHEDULE"},
		"negative delay":             {map[string]string{"RETRY_SCHEDULE": "-5s"}, "RETRY_SCHEDULE"},
		"absurdly long delay":        {map[string]string{"RETRY_SCHEDULE": "5s,1000h"}, "RETRY_SCHEDULE"},
		"too many retries":           {map[string]string{"RETRY_SCHEDULE": strings.Repeat("1s,", 20) + "1s"}, "at most"},
		"zero auto-disable limit":    {map[string]string{"AUTO_DISABLE_AFTER": "0"}, "AUTO_DISABLE_AFTER"},
		"non-numeric limit":          {map[string]string{"AUTO_DISABLE_AFTER": "lots"}, "AUTO_DISABLE_AFTER"},
		"reconcile too frequent":     {map[string]string{"RECONCILE_INTERVAL": "10ms"}, "RECONCILE_INTERVAL"},
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

func TestLoad_RetrySchedule(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  []time.Duration
	}{
		{"custom", "1s, 30s ,5m", []time.Duration{time.Second, 30 * time.Second, 5 * time.Minute}},
		{"a single retry", "10s", []time.Duration{10 * time.Second}},
		{"none turns retries off", "none", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setEnv(t, map[string]string{"RETRY_SCHEDULE": c.value})
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if len(cfg.RetrySchedule) != len(c.want) {
				t.Fatalf("schedule = %v, want %v", cfg.RetrySchedule, c.want)
			}
			for i := range c.want {
				if cfg.RetrySchedule[i] != c.want[i] {
					t.Fatalf("schedule = %v, want %v", cfg.RetrySchedule, c.want)
				}
			}
		})
	}
}

func TestLoad_DoesNotShareTheDefaultScheduleBetweenCalls(t *testing.T) {
	setEnv(t, nil)
	first, _ := Load()
	first.RetrySchedule[0] = time.Hour // a caller scribbling on its copy...
	second, _ := Load()
	if second.RetrySchedule[0] != 5*time.Second {
		t.Fatal("...must not change the defaults seen by later loads")
	}
}
