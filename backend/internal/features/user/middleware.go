package user

import (
	"context"
	"net/http"

	"github.com/samirthapa/tern/internal/shared/apperror"
	"github.com/samirthapa/tern/internal/shared/httpx"
)

type principalKey struct{}

func requirePrincipal(r *http.Request) (Principal, error) {
	p, ok := r.Context().Value(principalKey{}).(Principal)
	if !ok {
		return Principal{}, apperror.Unauthorized("invalid or expired session")
	}
	return p, nil
}

// RequireSession lets a request through only if it carries a valid session token.
func (m *Module) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.serveWithSession(w, r, next)
	})
}

// AuthenticateOr is for routes that accept both kinds of credential. A session token is resolved
// here, to the tenant of the user it belongs to; anything else goes to fallback, the API key
// authentication. Behind it, handlers cannot tell the two apart: both leave just the tenant in
// the request context, so a signed-in user and an API key see the same /v1 API.
func (m *Module) AuthenticateOr(fallback func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		apiKeys := fallback(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if HasSessionPrefix(httpx.BearerToken(r.Header.Get("Authorization"))) {
				m.serveWithSession(w, r, next)
				return
			}
			apiKeys.ServeHTTP(w, r)
		})
	}
}

func (m *Module) serveWithSession(w http.ResponseWriter, r *http.Request, next http.Handler) {
	p, err := m.svc.Authenticate(r.Context(), httpx.BearerToken(r.Header.Get("Authorization")))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	ctx := context.WithValue(r.Context(), principalKey{}, p)
	next.ServeHTTP(w, r.WithContext(httpx.WithTenantID(ctx, p.TenantID)))
}
