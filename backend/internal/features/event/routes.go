package event

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/samirthapa/tern/internal/shared/httpx"
)

// Module wires the event feature together and is the only thing main.go needs to know about.
type Module struct {
	svc  *Service
	ctrl *Controller
	// limitIngest guards the route that accepts events, which is the platform's hot path.
	limitIngest func(http.Handler) http.Handler
}

// NewModule builds the feature. limitIngest is middleware applied to POST /events (typically a
// per-tenant rate limit); nil means no limit.
func NewModule(pool *pgxpool.Pool, enqueuer Enqueuer, limitIngest func(http.Handler) http.Handler) *Module {
	if limitIngest == nil {
		limitIngest = func(next http.Handler) http.Handler { return next }
	}
	svc := NewService(NewRepository(pool), enqueuer)
	return &Module{svc: svc, ctrl: NewController(svc), limitIngest: limitIngest}
}

// RegisterRoutes expects r to already be guarded by the tenant Authenticate middleware.
//
// Routes are registered flat rather than under r.Route("/events"): the delivery feature also
// serves /events/{eventID}/deliveries, and chi cannot mount two routers on the same prefix.
func (m *Module) RegisterRoutes(r chi.Router) {
	r.With(m.limitIngest).Post("/events", httpx.Handle(m.ctrl.create))
	r.Get("/events/{eventID}", httpx.Handle(m.ctrl.get))
}
