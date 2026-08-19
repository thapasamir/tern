package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

var epoch = time.Unix(1_700_000_000, 0)

// newTestLimiter returns a limiter on an in-memory Redis whose clock the test controls.
func newTestLimiter(t *testing.T) (*Redis, *miniredis.Miniredis, *time.Time) {
	t.Helper()
	srv := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	now := epoch
	l := NewRedis(rdb, "test")
	l.now = func() time.Time { return now }
	return l, srv, &now
}

func mustAllow(t *testing.T, l *Redis, key string, limit Limit) Decision {
	t.Helper()
	d, err := l.Allow(context.Background(), key, limit)
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	return d
}

func TestAllow_ABurstIsAllowedThenRefused(t *testing.T) {
	l, _, _ := newTestLimiter(t)
	limit := Limit{Rate: 1, Burst: 3}

	for i, wantRemaining := range []int{2, 1, 0} {
		d := mustAllow(t, l, "k", limit)
		if !d.Allowed || d.Remaining != wantRemaining {
			t.Fatalf("request %d: %+v, want allowed with %d remaining", i+1, d, wantRemaining)
		}
	}

	d := mustAllow(t, l, "k", limit)
	if d.Allowed || d.Remaining != 0 {
		t.Fatalf("4th request: %+v, want refused with nothing remaining", d)
	}
	if d.RetryAfter != time.Second {
		t.Fatalf("RetryAfter = %v, want 1s (one token at 1/s)", d.RetryAfter)
	}
}

func TestAllow_TheBucketRefillsOverTime(t *testing.T) {
	l, _, now := newTestLimiter(t)
	limit := Limit{Rate: 2, Burst: 2} // one token every 500ms
	mustAllow(t, l, "k", limit)
	mustAllow(t, l, "k", limit)
	if mustAllow(t, l, "k", limit).Allowed {
		t.Fatal("bucket should be empty")
	}

	*now = epoch.Add(400 * time.Millisecond) // not yet a whole token
	d := mustAllow(t, l, "k", limit)
	if d.Allowed {
		t.Fatal("allowed after 400ms, but a token takes 500ms")
	}
	if d.RetryAfter > 150*time.Millisecond || d.RetryAfter <= 0 {
		t.Fatalf("RetryAfter = %v, want about 100ms more", d.RetryAfter)
	}

	*now = epoch.Add(500 * time.Millisecond)
	if d := mustAllow(t, l, "k", limit); !d.Allowed {
		t.Fatalf("refused after a full token has accrued: %+v", d)
	}
}

func TestAllow_RefillNeverExceedsTheBurst(t *testing.T) {
	l, _, now := newTestLimiter(t)
	limit := Limit{Rate: 10, Burst: 3}
	mustAllow(t, l, "k", limit)

	*now = epoch.Add(time.Hour) // far longer than needed to refill
	allowed := 0
	for i := 0; i < 10; i++ {
		if mustAllow(t, l, "k", limit).Allowed {
			allowed++
		}
	}
	if allowed != 3 {
		t.Fatalf("%d requests allowed after a long idle time, want exactly the burst of 3", allowed)
	}
}

func TestAllow_FractionalRates(t *testing.T) {
	l, _, _ := newTestLimiter(t)
	limit := Limit{Rate: 0.5, Burst: 1} // one token every 2 seconds
	mustAllow(t, l, "k", limit)
	d := mustAllow(t, l, "k", limit)
	if d.Allowed || d.RetryAfter != 2*time.Second {
		t.Fatalf("%+v, want refused with a 2s wait", d)
	}
}

func TestAllow_KeysAreIndependent(t *testing.T) {
	l, _, _ := newTestLimiter(t)
	limit := Limit{Rate: 1, Burst: 1}
	mustAllow(t, l, "tenant-a", limit)
	if mustAllow(t, l, "tenant-a", limit).Allowed {
		t.Fatal("tenant-a should be out of tokens")
	}
	if !mustAllow(t, l, "tenant-b", limit).Allowed {
		t.Fatal("one tenant's traffic must not use up another's tokens")
	}
}

func TestAllow_ResetAfterSaysWhenTheBucketIsFull(t *testing.T) {
	l, _, _ := newTestLimiter(t)
	limit := Limit{Rate: 1, Burst: 4}
	if d := mustAllow(t, l, "k", limit); d.ResetAfter != time.Second {
		t.Fatalf("ResetAfter = %v after taking 1 of 4 tokens at 1/s, want 1s", d.ResetAfter)
	}
	if d := mustAllow(t, l, "k", limit); d.ResetAfter != 2*time.Second {
		t.Fatalf("ResetAfter = %v after taking 2 of 4, want 2s", d.ResetAfter)
	}
}

func TestAllow_UnlimitedNeverTouchesRedis(t *testing.T) {
	l, srv, _ := newTestLimiter(t)
	for i := 0; i < 1000; i++ {
		if d := mustAllow(t, l, "k", Limit{Rate: 0, Burst: 5}); !d.Allowed {
			t.Fatal("an unlimited limit refused a request")
		}
	}
	if len(srv.Keys()) != 0 {
		t.Fatalf("unlimited limits left keys in Redis: %v", srv.Keys())
	}
}

func TestAllow_ConcurrentCallersCannotSpendTheSameTokenTwice(t *testing.T) {
	l, _, _ := newTestLimiter(t) // the clock is frozen, so no tokens refill during the test
	limit := Limit{Rate: 1, Burst: 10}

	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := l.Allow(context.Background(), "k", limit)
			if err != nil {
				t.Error(err)
				return
			}
			if d.Allowed {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 10 {
		t.Fatalf("%d of 60 concurrent requests were allowed, want exactly the burst of 10", allowed.Load())
	}
}

func TestAllow_IdleBucketsExpire(t *testing.T) {
	l, srv, _ := newTestLimiter(t)
	mustAllow(t, l, "k", Limit{Rate: 1, Burst: 5})

	if ttl := srv.TTL("test:k"); ttl <= 0 {
		t.Fatalf("bucket has no expiry (ttl %v): idle tenants would leak keys forever", ttl)
	}
	srv.FastForward(time.Minute)
	if srv.Exists("test:k") {
		t.Fatal("an idle bucket should have expired")
	}
}

func TestAllow_AClockGoingBackwardsDoesNotRefill(t *testing.T) {
	l, _, now := newTestLimiter(t)
	limit := Limit{Rate: 1, Burst: 1}
	mustAllow(t, l, "k", limit)

	*now = epoch.Add(-time.Hour) // two servers whose clocks disagree
	if mustAllow(t, l, "k", limit).Allowed {
		t.Fatal("a backwards clock must not mint tokens")
	}
}

func TestPerSecond(t *testing.T) {
	cases := []struct {
		rate      float64
		wantBurst int
		unlimited bool
	}{{100, 200, false}, {25, 50, false}, {0.4, 1, false}, {1, 2, false}, {0, 1, true}, {-5, 1, true}}
	for _, c := range cases {
		l := PerSecond(c.rate)
		if l.Burst != c.wantBurst || l.Unlimited() != c.unlimited {
			t.Errorf("PerSecond(%v) = %+v (unlimited %v), want burst %d unlimited %v", c.rate, l, l.Unlimited(), c.wantBurst, c.unlimited)
		}
	}
}

func TestPerMinute(t *testing.T) {
	l := PerMinute(30)
	if l.Rate != 0.5 || l.Burst != 30 {
		t.Fatalf("PerMinute(30) = %+v, want 0.5 tokens/s and a burst of 30", l)
	}
	if !PerMinute(0).Unlimited() {
		t.Fatal("PerMinute(0) must be unlimited")
	}
}
