package tenant

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"

	"github.com/samirthapa/tern/internal/shared/apperror"
	"github.com/samirthapa/tern/internal/shared/httpx"
)

// Authenticate requires a valid tenant API key and stores the tenant ID in the request context.
func (m *Module) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := httpx.BearerToken(r.Header.Get("Authorization"))
		tenantID, err := m.svc.Authenticate(r.Context(), token)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(httpx.WithTenantID(r.Context(), tenantID)))
	})
}

// RequireAdmin guards operator-only routes with the static ADMIN_TOKEN.
func (m *Module) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := httpx.BearerToken(r.Header.Get("Authorization"))
		// Compare fixed-length digests so neither content nor length leaks through timing.
		got := sha256.Sum256([]byte(token))
		if token == "" || subtle.ConstantTimeCompare(got[:], m.adminDigest[:]) != 1 {
			httpx.Error(w, r, apperror.Unauthorized("invalid or missing admin token"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
