package httpx

import (
	"context"
	"net/http"
	"time"
)

// Checker reports whether a dependency is reachable.
type Checker func(ctx context.Context) error

// Health handles liveness (/healthz): the process is up.
func Health(w http.ResponseWriter, _ *http.Request) {
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready handles readiness (/readyz): all named dependencies respond.
func Ready(checks map[string]Checker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		results := make(map[string]string, len(checks))
		status := http.StatusOK
		for name, check := range checks {
			if err := check(ctx); err != nil {
				results[name] = "down"
				status = http.StatusServiceUnavailable
				continue
			}
			results[name] = "up"
		}
		JSON(w, status, map[string]any{"status": statusText(status), "checks": results})
	}
}

func statusText(code int) string {
	if code == http.StatusOK {
		return "ready"
	}
	return "not_ready"
}
