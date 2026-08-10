package httpx

import (
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/samirthapa/tern/internal/shared/apperror"
	"github.com/samirthapa/tern/internal/shared/ratelimit"
)

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
// limiting for a while. An unlimited limit turns the middleware into a no-op.
func RateLimit(l ratelimit.Limiter, scope string, limit ratelimit.Limit, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if limit.Unlimited() {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantID, ok := TenantID(r.Context())
			if !ok {
				next.ServeHTTP(w, r) // not an authenticated route: nobody to limit
				return
			}

			d, err := l.Allow(r.Context(), scope+":"+tenantID.String(), limit)
			if err != nil {
				log.Error("rate limiter failed; letting the request through",
					"scope", scope, "tenant_id", tenantID, "error", err)
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
