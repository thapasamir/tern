// Package api assembles the HTTP API: the middleware, the health checks and the routes of every
// feature. Building it from a Deps value, instead of inside main, lets tests construct the complete
// router, and check it against the OpenAPI specification, without a database or Redis.
package api

import (
	"context"
	_ "embed"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/samirthapa/tern/internal/features/delivery"
	"github.com/samirthapa/tern/internal/features/endpoint"
	"github.com/samirthapa/tern/internal/features/event"
	"github.com/samirthapa/tern/internal/features/stats"
	"github.com/samirthapa/tern/internal/features/tenant"
	"github.com/samirthapa/tern/internal/shared/config"
	"github.com/samirthapa/tern/internal/shared/httpx"
	"github.com/samirthapa/tern/internal/shared/queue"
	"github.com/samirthapa/tern/internal/shared/ratelimit"
	"github.com/samirthapa/tern/internal/shared/secretbox"
	"github.com/samirthapa/tern/internal/shared/urlguard"
)

// openAPISpec is the API's specification, served at /openapi.yaml. A test keeps it in step with
// the routes below, so it cannot silently go out of date.
//
//go:embed openapi.yaml
var openAPISpec []byte

// Deps is what the API is built from. The observers and the metrics handler are optional.
type Deps struct {
	Pool   *pgxpool.Pool
	Redis  *redis.Client
	Config config.Config
	Log    *slog.Logger
	// Secrets encrypts endpoint signing secrets at rest.
	Secrets *secretbox.Box

	// HTTPObserver, if set, is told about every request, for metrics.
	HTTPObserver httpx.HTTPObserver
	// RateLimitObserver, if set, is told about every request a rate limit refuses.
	RateLimitObserver httpx.RateLimitObserver
	// MetricsHandler, if set, is served at GET /metrics, protected by the admin token.
	MetricsHandler http.Handler
}

// NewRouter builds the API.
func NewRouter(d Deps) *chi.Mux {
	r := chi.NewRouter()
	r.Use(httpx.RequestID)
	if d.HTTPObserver != nil {
		r.Use(httpx.Instrument(d.HTTPObserver))
	}
	r.Use(httpx.Logger(d.Log), httpx.Recoverer(d.Log))

	r.Get("/healthz", httpx.Health)
	r.Get("/readyz", httpx.Ready(map[string]httpx.Checker{
		"postgres": func(ctx context.Context) error { return d.Pool.Ping(ctx) },
		"redis":    func(ctx context.Context) error { return d.Redis.Ping(ctx).Err() },
	}))
	r.Get("/openapi.yaml", serveOpenAPI)

	// Feature modules. Each exposes RegisterRoutes; this is where they are mounted.
	tenantMod := tenant.NewModule(d.Pool, d.Config.AdminToken)
	endpointMod := endpoint.NewModule(d.Pool, d.Secrets, urlguard.NewValidator(d.Config.AllowLocalEndpoints))
	deliveryMod := delivery.NewModule(d.Pool, d.Secrets, queue.New(d.Redis, delivery.QueueName))
	statsMod := stats.NewModule(d.Pool)

	// Each tenant may only send so many events per second; the limit is kept in Redis, so it holds
	// across every API server.
	limiter := ratelimit.NewRedis(d.Redis, "tern:ratelimit")
	limitEvents := httpx.RateLimit(limiter, "events",
		ratelimit.PerSecond(float64(d.Config.EventsPerSecond)), d.Log, d.RateLimitObserver)
	eventMod := event.NewModule(d.Pool, deliveryMod.Service(), limitEvents) // events enqueue deliveries through it

	if d.MetricsHandler != nil {
		r.With(tenantMod.RequireAdmin).Method(http.MethodGet, "/metrics", d.MetricsHandler)
	}
	r.Route("/admin", func(r chi.Router) {
		r.Use(tenantMod.RequireAdmin)
		tenantMod.RegisterAdminRoutes(r)
	})
	// Every tenant-facing feature registers inside this group, behind API key authentication.
	r.Route("/v1", func(r chi.Router) {
		r.Use(tenantMod.Authenticate)
		tenantMod.RegisterRoutes(r)
		endpointMod.RegisterRoutes(r)
		eventMod.RegisterRoutes(r)
		deliveryMod.RegisterRoutes(r)
		statsMod.RegisterRoutes(r)
	})
	return r
}

func serveOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(openAPISpec)
}
