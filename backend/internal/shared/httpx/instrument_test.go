package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type observedRequest struct {
	method, route string
	status        int
	took          time.Duration
}

type fakeHTTPObserver struct{ requests []observedRequest }

func (f *fakeHTTPObserver) RequestFinished(method, route string, status int, d time.Duration) {
	f.requests = append(f.requests, observedRequest{method, route, status, d})
}

func newInstrumentedRouter(obs HTTPObserver) chi.Router {
	r := chi.NewRouter()
	r.Use(Instrument(obs))
	r.Get("/v1/endpoints/{endpointID}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Post("/v1/events", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })
	r.Get("/implicit", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("no WriteHeader call")) })
	r.Get("/slow", func(w http.ResponseWriter, _ *http.Request) { time.Sleep(30 * time.Millisecond) })
	return r
}

func do(r http.Handler, method, path string) {
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, path, nil))
}

func TestInstrument_ReportsTheRoutePatternNotTheURL(t *testing.T) {
	obs := &fakeHTTPObserver{}
	r := newInstrumentedRouter(obs)

	do(r, http.MethodGet, "/v1/endpoints/0191a7e0-aaaa-7000-8000-000000000001")
	do(r, http.MethodGet, "/v1/endpoints/0191a7e0-bbbb-7000-8000-000000000002")

	if len(obs.requests) != 2 {
		t.Fatalf("observed %d requests, want 2", len(obs.requests))
	}
	for _, req := range obs.requests {
		if req.route != "/v1/endpoints/{endpointID}" || req.method != "GET" || req.status != 200 {
			t.Fatalf("observed %+v, want the pattern /v1/endpoints/{endpointID}: one time series for all IDs", req)
		}
	}
}

func TestInstrument_ReportsTheStatusTheHandlerSet(t *testing.T) {
	obs := &fakeHTTPObserver{}
	do(newInstrumentedRouter(obs), http.MethodPost, "/v1/events")
	if got := obs.requests[0]; got.status != http.StatusAccepted || got.route != "/v1/events" || got.method != "POST" {
		t.Fatalf("observed %+v, want POST /v1/events 202", got)
	}
}

func TestInstrument_ADefault200IsReportedAs200(t *testing.T) {
	obs := &fakeHTTPObserver{}
	do(newInstrumentedRouter(obs), http.MethodGet, "/implicit")
	if got := obs.requests[0].status; got != http.StatusOK {
		t.Fatalf("status = %d for a handler that never called WriteHeader, want 200", got)
	}
}

func TestInstrument_UnknownRoutesShareOneLabel(t *testing.T) {
	obs := &fakeHTTPObserver{}
	r := newInstrumentedRouter(obs)
	do(r, http.MethodGet, "/wp-login.php")
	do(r, http.MethodGet, "/.env")
	do(r, http.MethodGet, "/a/b/c/d")

	for _, req := range obs.requests {
		if req.route != unmatchedRoute || req.status != http.StatusNotFound {
			t.Fatalf("observed %+v, want a 404 under the single %q label: probing URLs must not create time series", req, unmatchedRoute)
		}
	}
}

func TestInstrument_MeasuresHowLongTheRequestTook(t *testing.T) {
	obs := &fakeHTTPObserver{}
	do(newInstrumentedRouter(obs), http.MethodGet, "/slow")
	if got := obs.requests[0].took; got < 30*time.Millisecond || got > 2*time.Second {
		t.Fatalf("took %v, want at least the handler's 30ms", got)
	}
}
