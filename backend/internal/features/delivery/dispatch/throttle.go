package dispatch

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/features/delivery"
	"github.com/samirthapa/tern/internal/shared/ratelimit"
)

// maxThrottleSpread is how much extra a throttled delivery may be delayed, as a fraction of the
// wait. Without it, every delivery held back at the same moment would come back at the same
// moment and be throttled again together.
const maxThrottleSpread = 0.25

// Throttle limits how fast deliveries are sent to a single endpoint, so that a burst of events
// cannot flood a customer's server.
type Throttle interface {
	// Wait returns zero if a delivery to the endpoint may be sent now, using up a slot, or else how
	// long to wait before asking again. It uses nothing up when it says to wait.
	Wait(ctx context.Context, endpointID uuid.UUID) (time.Duration, error)
}

// NewThrottle returns a Throttle that gives each endpoint its own bucket of the given limit.
func NewThrottle(l ratelimit.Limiter, limit ratelimit.Limit) Throttle {
	return limiterThrottle{limiter: l, limit: limit}
}

type limiterThrottle struct {
	limiter ratelimit.Limiter
	limit   ratelimit.Limit
}

func (t limiterThrottle) Wait(ctx context.Context, endpointID uuid.UUID) (time.Duration, error) {
	d, err := t.limiter.Allow(ctx, "endpoint:"+endpointID.String(), t.limit)
	if err != nil || d.Allowed {
		return 0, err
	}
	return d.RetryAfter, nil
}

// holdBack returns how long to delay the delivery because its endpoint is already receiving as
// many deliveries as it should, or zero if it may go ahead.
//
// It fails open: if the throttle cannot be consulted, the delivery goes ahead, because holding
// back every delivery while the limiter is broken would be worse than not limiting for a while.
// An endpoint that is disabled is not throttled, since nothing is sent to it anyway.
func (p *Processor) holdBack(ctx context.Context, w delivery.Work) time.Duration {
	if p.cfg.Throttle == nil || !w.EndpointActive {
		return 0
	}
	wait, err := p.cfg.Throttle.Wait(ctx, w.EndpointID)
	if err != nil {
		p.log.Error("endpoint throttle failed; sending anyway", "endpoint_id", w.EndpointID, "error", err)
		return 0
	}
	if wait <= 0 {
		return 0
	}
	return wait + time.Duration(rand.Float64()*maxThrottleSpread*float64(wait))
}
