package delivery

import (
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/samirthapa/tern/internal/shared/httpx"
	"github.com/samirthapa/tern/internal/shared/secretbox"
)

// Module wires the API side of the delivery feature. The worker builds a Processor instead
// (see NewProcessor) and does not serve HTTP.
type Module struct {
	svc  *Service
	ctrl *Controller
}

func NewModule(pool *pgxpool.Pool, box *secretbox.Box, jobs JobQueue) *Module {
	svc := NewService(NewRepository(pool, box), jobs)
	return &Module{svc: svc, ctrl: NewController(svc)}
}

// Service exposes the service so the event feature can enqueue deliveries through its own
// small interface, wired in main.go.
func (m *Module) Service() *Service { return m.svc }

// RegisterRoutes expects r to already be guarded by the tenant Authenticate middleware.
// Registered flat because the event feature owns the other /events routes (see its routes.go).
func (m *Module) RegisterRoutes(r chi.Router) {
	r.Get("/deliveries", httpx.Handle(m.ctrl.list))
	r.Get("/events/{eventID}/deliveries", httpx.Handle(m.ctrl.listByEvent))
	r.Get("/deliveries/{deliveryID}", httpx.Handle(m.ctrl.get))
	r.Post("/deliveries/{deliveryID}/replay", httpx.Handle(m.ctrl.replay))
	// The endpoint feature owns the other /endpoints routes (see its routes.go).
	r.Post("/endpoints/{endpointID}/replay", httpx.Handle(m.ctrl.replayFailed))
}
