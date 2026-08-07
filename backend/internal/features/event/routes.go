package event

import (
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/samirthapa/tern/internal/shared/httpx"
)

// Module wires the event feature together and is the only thing main.go needs to know about.
type Module struct {
	svc  *Service
	ctrl *Controller
}

func NewModule(pool *pgxpool.Pool, enqueuer Enqueuer) *Module {
	svc := NewService(NewRepository(pool), enqueuer)
	return &Module{svc: svc, ctrl: NewController(svc)}
}

// RegisterRoutes expects r to already be guarded by the tenant Authenticate middleware.
//
// Routes are registered flat rather than under r.Route("/events"): the delivery feature also
// serves /events/{eventID}/deliveries, and chi cannot mount two routers on the same prefix.
func (m *Module) RegisterRoutes(r chi.Router) {
	r.Post("/events", httpx.Handle(m.ctrl.create))
	r.Get("/events/{eventID}", httpx.Handle(m.ctrl.get))
}
