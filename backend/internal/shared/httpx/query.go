package httpx

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/shared/apperror"
)

// QueryUUID reads an optional UUID query parameter. It returns nil when the parameter is absent.
func QueryUUID(r *http.Request, name string) (*uuid.UUID, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, apperror.Invalid("invalid " + name)
	}
	return &id, nil
}

// QueryTime reads an optional RFC 3339 timestamp query parameter, such as 2026-08-01T12:00:00Z.
// It returns nil when the parameter is absent. A "+" in a UTC offset must be sent as %2B.
func QueryTime(r *http.Request, name string) (*time.Time, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, apperror.Invalid(name + " must be an RFC 3339 timestamp, for example 2026-08-01T12:00:00Z")
	}
	return &t, nil
}
