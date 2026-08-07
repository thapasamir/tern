package httpx

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/shared/apperror"
)

// HandlerFunc is a controller method that reports failure by returning an error
// instead of writing the error response itself.
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// Handle adapts a HandlerFunc to http.HandlerFunc. Any returned error is mapped to a
// client-safe JSON response in one place, so controllers just `return err`.
func Handle(fn HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			Error(w, r, err)
		}
	}
}

// PathUUID parses the named chi URL parameter as a UUID. label names the resource in the
// error message, e.g. "endpoint id".
func PathUUID(r *http.Request, param, label string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, param))
	if err != nil {
		return uuid.Nil, apperror.Invalid("invalid " + label)
	}
	return id, nil
}
