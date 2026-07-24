// Package httpx contains HTTP helpers: JSON responses, error mapping and middleware.
package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/samirthapa/webhook-platform/internal/shared/apperror"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code      apperror.Code `json:"code"`
	Message   string        `json:"message"`
	RequestID string        `json:"request_id,omitempty"`
}

// JSON writes v as a JSON response with the given status.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("httpx: encode response", "error", err)
	}
}

// Error maps err to a client-safe JSON error. Internal causes are logged, not returned.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	ae := apperror.From(err)
	reqID := RequestIDFrom(r.Context())

	if ae.Code == apperror.CodeInternal {
		slog.Error("request failed", "error", ae.Err, "request_id", reqID, "path", r.URL.Path)
	}
	JSON(w, ae.HTTPStatus(), errorBody{Error: errorDetail{
		Code:      ae.Code,
		Message:   ae.Message,
		RequestID: reqID,
	}})
}

// DecodeJSON reads a JSON body, rejecting unknown fields and oversized payloads.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return apperror.Invalid("malformed JSON body")
	}
	return nil
}
