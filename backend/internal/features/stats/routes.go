package stats

import (
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/samirthapa/tern/internal/shared/httpx"
)

// Module wires the stats feature together and is the only thing main.go needs to know about.
type Module struct {
	ctrl *Controller
}

func NewModule(pool *pgxpool.Pool) *Module {
	return &Module{ctrl: NewController(NewService(NewRepository(pool)))}
}

// RegisterRoutes expects r to already be guarded by the tenant Authenticate middleware.
func (m *Module) RegisterRoutes(r chi.Router) {
	r.Get("/stats/deliveries", httpx.Handle(m.ctrl.deliveries))
}
