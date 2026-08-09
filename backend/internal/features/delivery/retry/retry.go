// Package retry decides whether and when a failed delivery is attempted again.
package retry

import (
	"math/rand/v2"
	"slices"
	"time"
)

const (
	// defaultJitter spreads each delay by up to +-20%, so that the retries of many deliveries that
	// failed together (say, during a customer's outage) do not all hit the endpoint at the same
	// instant when it comes back.
	defaultJitter = 0.2
	// DefaultMaxRetryAfter caps how long a server's Retry-After can postpone a retry: a bogus
	// "come back in a year" must not park a delivery forever.
	DefaultMaxRetryAfter = time.Hour
)

// Policy decides whether and when a failed delivery is attempted again.
//
// The schedule lists the delay after each failed attempt: schedule[0] follows the first failure,
// schedule[1] the second, and so on. Once it runs out the delivery fails for good, so a schedule
// of n delays allows n+1 attempts in total.
type Policy struct {
	schedule      []time.Duration
	jitter        float64
	maxRetryAfter time.Duration
	rand          func() float64 // in [0, 1); replaceable so tests are deterministic
}

// NewPolicy returns a policy with the given delays between attempts. An empty schedule
// means deliveries are never retried.
func NewPolicy(schedule []time.Duration) Policy {
	return Policy{
		schedule:      slices.Clone(schedule),
		jitter:        defaultJitter,
		maxRetryAfter: DefaultMaxRetryAfter,
		rand:          rand.Float64,
	}
}

// WithJitter returns a copy of the policy whose delays vary by up to +-fraction (0.2 is +-20%).
// Zero makes every delay exactly the schedule's, which is what tests that check times want.
func (p Policy) WithJitter(fraction float64) Policy {
	p.jitter = fraction
	return p
}

// MaxAttempts is the total number of attempts a delivery gets, the first one included.
func (p Policy) MaxAttempts() int { return len(p.schedule) + 1 }

// NextDelay returns how long to wait before the next attempt after attempt number failedAttempt
// (1 for the first attempt) failed. ok is false when no attempts are left.
//
// retryAfter is the server's own request ("Retry-After"), or zero. It is honoured but capped, and
// never makes the wait shorter than the schedule says: a server asking for less than our backoff
// does not get hammered.
func (p Policy) NextDelay(failedAttempt int, retryAfter time.Duration) (delay time.Duration, ok bool) {
	if failedAttempt < 1 || failedAttempt > len(p.schedule) {
		return 0, false
	}
	base := p.schedule[failedAttempt-1]
	delay = time.Duration(float64(base) * (1 + p.jitter*(2*p.rand()-1)))

	if retryAfter > 0 {
		delay = max(delay, min(retryAfter, p.maxRetryAfter))
	}
	return delay, true
}
