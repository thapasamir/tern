package endpoint

import (
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/samirthapa/tern/internal/shared/httpx"
)

// Module wires the endpoint feature together and is the only thing main.go needs to know about.
type Module struct {
	svc  *Service
	ctrl *Controller
}

func NewModule(pool *pgxpool.Pool, urls URLValidator) *Module {
	svc := NewService(NewRepository(pool), urls)
	return &Module{svc: svc, ctrl: NewController(svc)}
}

// Service exposes the service so other features (e.g. delivery) can depend on it through
// their own small interfaces, wired in main.go.
func (m *Module) Service() *Service { return m.svc }

// RegisterRoutes expects r to already be guarded by the tenant Authenticate middleware.
func (m *Module) RegisterRoutes(r chi.Router) {
	r.Route("/endpoints", func(r chi.Router) {
		r.Post("/", httpx.Handle(m.ctrl.create))
		r.Get("/", httpx.Handle(m.ctrl.list))

		r.Route("/{endpointID}", func(r chi.Router) {
			r.Get("/", httpx.Handle(m.ctrl.get))
			r.Patch("/", httpx.Handle(m.ctrl.update))
			r.Delete("/", httpx.Handle(m.ctrl.delete))

			r.Post("/rotate-secret", httpx.Handle(m.ctrl.rotateSecret))
			r.Post("/enable", httpx.Handle(m.ctrl.enable))
			r.Post("/disable", httpx.Handle(m.ctrl.disable))
		})
	})
}
