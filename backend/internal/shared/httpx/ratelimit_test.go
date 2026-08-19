package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/shared/ratelimit"
	"github.com/samirthapa/tern/internal/shared/testutil"
)

// fakeLimiter returns a canned decision and records what it was asked.
type fakeLimiter struct {
	decision ratelimit.Decision
	err      error
	keys     []string
	limits   []ratelimit.Limit
}

func (f *fakeLimiter) Allow(_ context.Context, key string, limit ratelimit.Limit) (ratelimit.Decision, error) {
	f.keys = append(f.keys, key)
	f.limits = append(f.limits, limit)
	return f.decision, f.err
}

// serve runs one request through the middleware and reports whether the next handler ran.
func serve(t *testing.T, mw func(http.Handler) http.Handler, tenant *uuid.UUID) (rec *httptest.ResponseRecorder, reached bool) {
	t.Helper()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusAccepted)
	})
	req := httptest.NewRequest(http.MethodPost, "/events", nil)
	if tenant != nil {
		req = req.WithContext(WithTenantID(req.Context(), *tenant))
	}
	rec = httptest.NewRecorder()
	mw(next).ServeHTTP(rec, req)
	return rec, reached
}

var testLimit = ratelimit.Limit{Rate: 10, Burst: 20}

func TestRateLimit_AllowedRequestsPassWithHeaders(t *testing.T) {
	l := &fakeLimiter{decision: ratelimit.Decision{Allowed: true, Remaining: 17, ResetAfter: 1500 * time.Millisecond}}
	tenant := uuid.New()
	rec, reached := serve(t, RateLimit(l, "events", testLimit, testutil.QuietLogger(), nil), &tenant)

	if !reached || rec.Code != http.StatusAccepted {
		t.Fatalf("status %d, reached %v; an allowed request must reach the handler", rec.Code, reached)
	}
	h := rec.Header()
	if h.Get("RateLimit-Limit") != "20" || h.Get("RateLimit-Remaining") != "17" || h.Get("RateLimit-Reset") != "2" {
		t.Fatalf("headers = %v, want limit 20, remaining 17, reset 2 (1.5s rounded up)", h)
	}
	if h.Get("Retry-After") != "" {
		t.Fatal("Retry-After belongs on refusals only")
	}
}

func TestRateLimit_RefusesWith429AndRetryAfter(t *testing.T) {
	l := &fakeLimiter{decision: ratelimit.Decision{Allowed: false, RetryAfter: 2300 * time.Millisecond, ResetAfter: 5 * time.Second}}
	tenant := uuid.New()
	rec, reached := serve(t, RateLimit(l, "events", testLimit, testutil.QuietLogger(), nil), &tenant)

	if reached {
		t.Fatal("a refused request must not reach the handler")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "3" {
		t.Fatalf("Retry-After = %q, want 3 (2.3s rounded up)", got)
	}
	if rec.Header().Get("RateLimit-Remaining") != "0" {
		t.Fatalf("RateLimit-Remaining = %q on a refusal, want 0", rec.Header().Get("RateLimit-Remaining"))
	}
	if body := rec.Body.String(); !strings.Contains(body, `"rate_limited"`) || !strings.Contains(body, "retry in 3 seconds") {
		t.Fatalf("body = %s, want the standard error shape with code rate_limited", body)
	}
}

func TestRateLimit_RetryAfterIsAtLeastOneSecond(t *testing.T) {
	l := &fakeLimiter{decision: ratelimit.Decision{Allowed: false, RetryAfter: 20 * time.Millisecond}}
	tenant := uuid.New()
	rec, _ := serve(t, RateLimit(l, "events", testLimit, testutil.QuietLogger(), nil), &tenant)
	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want 1: a client told to wait 0 seconds would retry in a tight loop", got)
	}
}

func TestRateLimit_BucketsAreKeyedByScopeAndTenant(t *testing.T) {
	l := &fakeLimiter{decision: ratelimit.Decision{Allowed: true}}
	a, b := uuid.New(), uuid.New()
	events := RateLimit(l, "events", testLimit, testutil.QuietLogger(), nil)
	other := RateLimit(l, "other", testLimit, testutil.QuietLogger(), nil)

	serve(t, events, &a)
	serve(t, events, &b)
	serve(t, other, &a)
	want := []string{"events:" + a.String(), "events:" + b.String(), "other:" + a.String()}
	for i := range want {
		if l.keys[i] != want[i] {
			t.Fatalf("keys = %v, want %v: tenants and scopes must not share a bucket", l.keys, want)
		}
	}
	if l.limits[0] != testLimit {
		t.Fatalf("limit passed on = %+v, want %+v", l.limits[0], testLimit)
	}
}

func TestRateLimit_FailsOpenWhenTheLimiterBreaks(t *testing.T) {
	l := &fakeLimiter{err: errors.New("redis: connection refused")}
	tenant := uuid.New()
	rec, reached := serve(t, RateLimit(l, "events", testLimit, testutil.QuietLogger(), nil), &tenant)
	if !reached || rec.Code != http.StatusAccepted {
		t.Fatalf("status %d, reached %v; a broken limiter must not refuse customers' events", rec.Code, reached)
	}
}

func TestRateLimit_LeavesUnauthenticatedRequestsAlone(t *testing.T) {
	l := &fakeLimiter{decision: ratelimit.Decision{Allowed: false}}
	_, reached := serve(t, RateLimit(l, "events", testLimit, testutil.QuietLogger(), nil), nil)
	if !reached || len(l.keys) != 0 {
		t.Fatalf("reached %v, limiter calls %d; with no tenant there is nobody to limit", reached, len(l.keys))
	}
}

func TestRateLimit_UnlimitedIsANoOp(t *testing.T) {
	l := &fakeLimiter{decision: ratelimit.Decision{Allowed: false}}
	tenant := uuid.New()
	rec, reached := serve(t, RateLimit(l, "events", ratelimit.PerSecond(0), testutil.QuietLogger(), nil), &tenant)
	if !reached || len(l.keys) != 0 || rec.Header().Get("RateLimit-Limit") != "" {
		t.Fatal("an unlimited limit must not consult the limiter or add headers")
	}
}

type fakeRefusalObserver struct{ scopes []string }

func (f *fakeRefusalObserver) RateLimited(scope string) { f.scopes = append(f.scopes, scope) }

func TestRateLimit_ReportsRefusalsButNotAllowedRequests(t *testing.T) {
	tenant := uuid.New()
	obs := &fakeRefusalObserver{}

	allowed := &fakeLimiter{decision: ratelimit.Decision{Allowed: true}}
	serve(t, RateLimit(allowed, "events", testLimit, testutil.QuietLogger(), obs), &tenant)
	if len(obs.scopes) != 0 {
		t.Fatalf("observed %v for an allowed request, want nothing", obs.scopes)
	}

	refused := &fakeLimiter{decision: ratelimit.Decision{Allowed: false, RetryAfter: time.Second}}
	serve(t, RateLimit(refused, "events", testLimit, testutil.QuietLogger(), obs), &tenant)
	if len(obs.scopes) != 1 || obs.scopes[0] != "events" {
		t.Fatalf("observed %v, want one refusal in the scope \"events\"", obs.scopes)
	}
}

func TestRateLimitByClient_CountsAgainstTheRemoteAddressWithoutItsPort(t *testing.T) {
	l := &fakeLimiter{decision: ratelimit.Decision{Allowed: true, Remaining: 4}}
	mw := RateLimitByClient(l, "auth", testLimit, testutil.QuietLogger(), nil)

	for _, remote := range []string{"203.0.113.9:51000", "203.0.113.9:51001", "198.51.100.2:40000"} {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
		req.RemoteAddr = remote
		mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(httptest.NewRecorder(), req)
	}

	want := []string{"auth:203.0.113.9", "auth:203.0.113.9", "auth:198.51.100.2"}
	if strings.Join(l.keys, ",") != strings.Join(want, ",") {
		t.Fatalf("buckets = %v, want %v (one per address, not per connection)", l.keys, want)
	}
}

func TestRateLimitByClient_RefusesWhenTheBucketIsEmpty(t *testing.T) {
	l := &fakeLimiter{decision: ratelimit.Decision{Allowed: false, RetryAfter: time.Second}}
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "203.0.113.9:51000"
	rec := httptest.NewRecorder()
	reached := false
	RateLimitByClient(l, "auth", testLimit, testutil.QuietLogger(), nil)(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })).ServeHTTP(rec, req)

	if reached || rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d, reached %v; want 429 and the handler not run", rec.Code, reached)
	}
}
