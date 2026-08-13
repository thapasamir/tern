package httpx

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// HTTPObserver is told about every request the API handles, so that it can be measured. It is
// implemented by the metrics package.
type HTTPObserver interface {
	RequestFinished(method, route string, status int, d time.Duration)
}

// unmatchedRoute labels requests that matched no route. Using the raw path instead would give a
// scanner probing random URLs the power to create unlimited time series.
const unmatchedRoute = "unmatched"

// Instrument reports each request to obs once it has been handled: its method, the route pattern
// it matched (for example /v1/endpoints/{endpointID}, never the concrete URL), its status code
// and how long it took. It must wrap the router, because the pattern is only known after routing.
func Instrument(obs HTTPObserver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			route := unmatchedRoute
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				route = rc.RoutePattern()
			}
			obs.RequestFinished(r.Method, route, rec.status, time.Since(start))
		})
	}
}
