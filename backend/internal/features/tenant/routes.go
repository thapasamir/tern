package tenant

import (
	"crypto/sha256"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/samirthapa/tern/internal/shared/httpx"
)

// Module wires the tenant feature together and is the only thing main.go needs to know about.
type Module struct {
	svc         *Service
	ctrl        *Controller
	adminDigest [sha256.Size]byte
}

func NewModule(pool *pgxpool.Pool, adminToken string) *Module {
	svc := NewService(NewRepository(pool))
	return &Module{
		svc:         svc,
		ctrl:        NewController(svc),
		adminDigest: sha256.Sum256([]byte(adminToken)),
	}
}

// RegisterAdminRoutes expects r to already be guarded by RequireAdmin.
func (m *Module) RegisterAdminRoutes(r chi.Router) {
	r.Post("/tenants", httpx.Handle(m.ctrl.createTenant))
	r.Post("/tenants/{tenantID}/api-keys", httpx.Handle(m.ctrl.createAPIKeyForTenant))
}

// RegisterRoutes expects r to already be guarded by Authenticate.
func (m *Module) RegisterRoutes(r chi.Router) {
	r.Get("/tenant", httpx.Handle(m.ctrl.me))
	r.Get("/api-keys", httpx.Handle(m.ctrl.listAPIKeys))
	r.Post("/api-keys", httpx.Handle(m.ctrl.createAPIKey))
	r.Delete("/api-keys/{keyID}", httpx.Handle(m.ctrl.revokeAPIKey))
}
