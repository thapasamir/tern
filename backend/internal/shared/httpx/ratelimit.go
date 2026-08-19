package httpx

import (
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/samirthapa/tern/internal/shared/apperror"
	"github.com/samirthapa/tern/internal/shared/ratelimit"
)

// RateLimitObserver is told when a request is refused, so that refusals can be measured.
type RateLimitObserver interface {
	RateLimited(scope string)
}

// RateLimit limits how often each tenant may call the routes it guards. It must run after the
// authentication middleware, which tells it who the tenant is. scope names what is being limited
// ("events"), so different routes get separate buckets.
//
// Every response from a guarded route carries RateLimit-Limit, RateLimit-Remaining and
// RateLimit-Reset (seconds until the bucket is full) so clients can pace themselves. A refused
// request gets 429 and a Retry-After header saying how many seconds to wait.
//
// If the limiter itself fails (Redis is down), the request is let through and the failure is
// logged: refusing customers' events because the limiter is broken would be worse than not
// limiting for a while. An unlimited limit turns the middleware into a no-op. obs is optional.
func RateLimit(l ratelimit.Limiter, scope string, limit ratelimit.Limit, log *slog.Logger, obs RateLimitObserver) func(http.Handler) http.Handler {
	return rateLimit(l, scope, limit, log, obs, func(r *http.Request) (string, bool) {
		tenantID, ok := TenantID(r.Context())
		return tenantID.String(), ok // not an authenticated route: nobody to limit
	})
}

// RateLimitByClient is RateLimit for routes that are reached before anyone has authenticated, such
// as logging in: it limits by the client's network address, which is how a password guesser is
// told apart from everyone else. It trusts the connection's remote address, so behind a reverse
// proxy the proxy must pass the real client address through (for example with chi's RealIP).
func RateLimitByClient(l ratelimit.Limiter, scope string, limit ratelimit.Limit, log *slog.Logger, obs RateLimitObserver) func(http.Handler) http.Handler {
	return rateLimit(l, scope, limit, log, obs, func(r *http.Request) (string, bool) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		return host, host != ""
	})
}

// rateLimit is the shared middleware. identify names who a request counts against, and reports
// false for a request that has nobody to limit.
func rateLimit(l ratelimit.Limiter, scope string, limit ratelimit.Limit, log *slog.Logger, obs RateLimitObserver,
	identify func(*http.Request) (string, bool)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if limit.Unlimited() {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			who, ok := identify(r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			d, err := l.Allow(r.Context(), scope+":"+who, limit)
			if err != nil {
				log.Error("rate limiter failed; letting the request through",
					"scope", scope, "who", who, "error", err)
				next.ServeHTTP(w, r)
				return
			}

			h := w.Header()
			h.Set("RateLimit-Limit", strconv.Itoa(limit.Burst))
			h.Set("RateLimit-Remaining", strconv.Itoa(d.Remaining))
			h.Set("RateLimit-Reset", strconv.Itoa(wholeSeconds(d.ResetAfter)))

			if !d.Allowed {
				wait := max(1, wholeSeconds(d.RetryAfter))
				h.Set("Retry-After", strconv.Itoa(wait))
				if obs != nil {
					obs.RateLimited(scope)
				}
				Error(w, r, apperror.New(apperror.CodeRateLimited,
					fmt.Sprintf("rate limit exceeded: retry in %d seconds", wait)))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// wholeSeconds rounds a duration up to whole seconds, as the headers require.
func wholeSeconds(d time.Duration) int {
	return int(math.Ceil(d.Seconds()))
}
