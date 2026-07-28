package httpx

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/samirthapa/webhook-platform/internal/shared/apperror"
)

const tenantIDKey ctxKey = iota + 100

// WithTenantID stores the authenticated tenant in ctx. Used by the auth middleware.
func WithTenantID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, tenantIDKey, id)
}

// TenantID returns the authenticated tenant of the request. ok is false on unauthenticated routes.
func TenantID(ctx context.Context) (id uuid.UUID, ok bool) {
	id, ok = ctx.Value(tenantIDKey).(uuid.UUID)
	return id, ok
}

// RequireTenantID returns the authenticated tenant, or an Unauthorized error when the route
// was reached without going through the auth middleware.
func RequireTenantID(r *http.Request) (uuid.UUID, error) {
	id, ok := TenantID(r.Context())
	if !ok {
		return uuid.Nil, apperror.Unauthorized("invalid or missing API key")
	}
	return id, nil
}

// BearerToken extracts the token from an "Authorization: Bearer <token>" header value.
func BearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}
