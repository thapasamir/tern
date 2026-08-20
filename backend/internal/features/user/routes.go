package user

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/samirthapa/tern/internal/shared/httpx"
	"github.com/samirthapa/tern/internal/shared/password"
)

// Module wires the user feature together and is the only thing main.go needs to know about.
type Module struct {
	svc  *Service
	ctrl *Controller
	// limitAttempts guards the routes that take a password, against guessing.
	limitAttempts func(http.Handler) http.Handler
}

// NewModule builds the feature. limitAttempts is middleware applied to signing up and in
// (typically a per-client rate limit); nil means no limit.
func NewModule(pool *pgxpool.Pool, limitAttempts func(http.Handler) http.Handler) *Module {
	if limitAttempts == nil {
		limitAttempts = func(next http.Handler) http.Handler { return next }
	}
	svc := NewService(NewRepository(pool), password.NewHasher(password.DefaultParams))
	return &Module{svc: svc, ctrl: NewController(svc), limitAttempts: limitAttempts}
}

// RegisterRoutes mounts the routes under /auth. Signing up and in are public; the others need a
// session.
func (m *Module) RegisterRoutes(r chi.Router) {
	r.Route("/auth", func(r chi.Router) {
		r.With(m.limitAttempts).Post("/signup", httpx.Handle(m.ctrl.signup))
		r.With(m.limitAttempts).Post("/login", httpx.Handle(m.ctrl.login))

		r.Group(func(r chi.Router) {
			r.Use(m.RequireSession)
			r.Post("/logout", httpx.Handle(m.ctrl.logout))
			r.Get("/me", httpx.Handle(m.ctrl.me))
		})
	})
}
