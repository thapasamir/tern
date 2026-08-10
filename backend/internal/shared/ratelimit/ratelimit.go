// Package ratelimit is a rate limiter on Redis, using the token bucket algorithm.
//
// Each key owns a bucket that holds up to Burst tokens and refills at Rate tokens per second.
// A request takes one token and is refused if the bucket is empty. That allows short bursts while
// holding the long-run rate to Rate. The bucket lives in Redis and is updated by one Lua script,
// so the check-and-take is atomic across any number of API servers and workers.
package ratelimit

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limit describes a bucket.
type Limit struct {
	// Rate is how many tokens are added per second. Zero or less means unlimited.
	Rate float64
	// Burst is the capacity of the bucket: how many requests may arrive at once.
	Burst int
}

// PerSecond returns a Limit of rate requests per second that allows bursts of twice that size.
// A rate of zero or less means unlimited.
func PerSecond(rate float64) Limit {
	return Limit{Rate: rate, Burst: max(1, int(math.Ceil(2*rate)))}
}

// Unlimited reports whether the limit is switched off.
func (l Limit) Unlimited() bool { return l.Rate <= 0 }

// Decision is the outcome of asking for one token.
type Decision struct {
	Allowed bool
	// Remaining is how many more requests would be allowed right now.
	Remaining int
	// RetryAfter is, for a refused request, how long to wait until one would be allowed.
	RetryAfter time.Duration
	// ResetAfter is how long until the bucket is full again.
	ResetAfter time.Duration
}

// Limiter hands out tokens.
type Limiter interface {
	// Allow takes one token from the bucket named key, creating it full if it does not exist.
	Allow(ctx context.Context, key string, limit Limit) (Decision, error)
}

// allowScript refills and then drains a bucket in one atomic step.
// KEYS: bucket. ARGV: rate (tokens/s), burst, now (unix ms), cost.
// Returns {allowed (0/1), remaining tokens, retry-after ms, reset-after ms}.
var allowScript = redis.NewScript(`
local rate  = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local now   = tonumber(ARGV[3])
local cost  = tonumber(ARGV[4])

local state  = redis.call('HMGET', KEYS[1], 'tokens', 'ts')
local tokens = tonumber(state[1])
local ts     = tonumber(state[2])
if tokens == nil then
  tokens = burst
  ts = now
end

-- Refill for the time that has passed. A clock that went backwards refills nothing.
tokens = math.min(burst, tokens + math.max(0, now - ts) * rate / 1000)

local allowed = 0
local retry_ms = 0
if tokens >= cost then
  tokens = tokens - cost
  allowed = 1
else
  retry_ms = math.ceil((cost - tokens) * 1000 / rate)
end

redis.call('HSET', KEYS[1], 'tokens', tostring(tokens), 'ts', tostring(now))
-- An idle bucket is full again after burst/rate seconds, so it can be forgotten after that.
redis.call('PEXPIRE', KEYS[1], math.ceil(burst * 1000 / rate) + 1000)

return {allowed, math.floor(tokens), retry_ms, math.ceil((burst - tokens) * 1000 / rate)}
`)

// Redis is a Limiter backed by a Redis server.
type Redis struct {
	rdb    *redis.Client
	prefix string
	now    func() time.Time
}

// NewRedis returns a Limiter whose keys are namespaced under prefix, e.g. "tern:ratelimit".
func NewRedis(rdb *redis.Client, prefix string) *Redis {
	return &Redis{rdb: rdb, prefix: prefix, now: time.Now}
}

// Allow implements Limiter. An unlimited Limit always allows and never touches Redis.
func (r *Redis) Allow(ctx context.Context, key string, limit Limit) (Decision, error) {
	if limit.Unlimited() {
		return Decision{Allowed: true, Remaining: limit.Burst}, nil
	}

	res, err := allowScript.Run(ctx, r.rdb, []string{r.prefix + ":" + key},
		limit.Rate, limit.Burst, r.now().UnixMilli(), 1).Int64Slice()
	if err != nil {
		return Decision{}, fmt.Errorf("ratelimit: %w", err)
	}
	if len(res) != 4 {
		return Decision{}, fmt.Errorf("ratelimit: unexpected script reply %v", res)
	}
	return Decision{
		Allowed:    res[0] == 1,
		Remaining:  int(res[1]),
		RetryAfter: time.Duration(res[2]) * time.Millisecond,
		ResetAfter: time.Duration(res[3]) * time.Millisecond,
	}, nil
}
