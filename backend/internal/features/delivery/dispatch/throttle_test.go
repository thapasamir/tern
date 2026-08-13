package dispatch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/features/delivery"
	"github.com/samirthapa/tern/internal/shared/ids"
	"github.com/samirthapa/tern/internal/shared/ratelimit"
)

// fakeLimiter answers every request with a canned decision and records what it was asked.
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

// fakeThrottle is a Throttle with a canned answer that counts how often it was consulted.
type fakeThrottle struct {
	wait  time.Duration
	err   error
	calls []uuid.UUID
}

func (f *fakeThrottle) Wait(_ context.Context, endpointID uuid.UUID) (time.Duration, error) {
	f.calls = append(f.calls, endpointID)
	return f.wait, f.err
}

func TestNewThrottle(t *testing.T) {
	endpoint := ids.New()
	limit := ratelimit.PerSecond(25)

	t.Run("an allowed delivery waits for nothing", func(t *testing.T) {
		l := &fakeLimiter{decision: ratelimit.Decision{Allowed: true}}
		wait, err := NewThrottle(l, limit).Wait(context.Background(), endpoint)
		if err != nil || wait != 0 {
			t.Fatalf("Wait = %v, %v; want 0, nil", wait, err)
		}
		if l.keys[0] != "endpoint:"+endpoint.String() || l.limits[0] != limit {
			t.Fatalf("asked %v with %+v; each endpoint needs its own bucket and the configured limit", l.keys, l.limits)
		}
	})
	t.Run("a refused delivery is told how long to wait", func(t *testing.T) {
		l := &fakeLimiter{decision: ratelimit.Decision{Allowed: false, RetryAfter: 300 * time.Millisecond}}
		wait, err := NewThrottle(l, limit).Wait(context.Background(), endpoint)
		if err != nil || wait != 300*time.Millisecond {
			t.Fatalf("Wait = %v, %v; want 300ms", wait, err)
		}
	})
	t.Run("a limiter failure is reported", func(t *testing.T) {
		l := &fakeLimiter{err: errors.New("redis down")}
		if _, err := NewThrottle(l, limit).Wait(context.Background(), endpoint); err == nil {
			t.Fatal("want the error, so the processor can decide to fail open")
		}
	})
}

func TestProcess_AThrottledDeliveryIsHeldBackWithoutCountingAnAttempt(t *testing.T) {
	f := newFixture(t)
	th := &fakeThrottle{wait: 10 * time.Second}
	f.proc.cfg.Throttle = th

	retryAt := f.process(t)

	// Held back by the wait plus a little spread, so the held-back deliveries do not return together.
	if earliest, latest := testNow.Add(10*time.Second), testNow.Add(12500*time.Millisecond); retryAt.Before(earliest) || retryAt.After(latest) {
		t.Fatalf("held back until %v, want between %v and %v", retryAt, earliest, latest)
	}
	if len(f.sender.requests) != 0 || len(f.repo.recorded) != 0 {
		t.Fatal("a throttled delivery must not be sent or recorded: it is not an attempt")
	}
	if len(f.health.failures)+len(f.health.successes)+len(f.health.disables) != 0 {
		t.Fatal("being throttled says nothing about the endpoint's health")
	}
}

func TestProcess_TheThrottleSpreadStaysWithinItsBounds(t *testing.T) {
	f := newFixture(t)
	f.proc.cfg.Throttle = &fakeThrottle{wait: 4 * time.Second}

	seen := map[time.Time]bool{}
	for i := 0; i < 200; i++ {
		retryAt := f.process(t)
		if d := retryAt.Sub(testNow); d < 4*time.Second || d > 5*time.Second {
			t.Fatalf("held back %v, want between 4s and 5s", d)
		}
		seen[retryAt] = true
	}
	if len(seen) < 20 {
		t.Fatalf("only %d distinct times in 200 draws: the spread is not random", len(seen))
	}
}

func TestProcess_ADeliveryThatMayGoAheadIsSent(t *testing.T) {
	f := newFixture(t)
	th := &fakeThrottle{}
	f.proc.cfg.Throttle = th

	f.process(t)
	if len(th.calls) != 1 || th.calls[0] != f.work.EndpointID {
		t.Fatalf("throttle consulted for %v, want exactly once for the delivery's endpoint", th.calls)
	}
	if len(f.sender.requests) != 1 {
		t.Fatalf("sent %d requests, want 1", len(f.sender.requests))
	}
}

func TestProcess_ABrokenThrottleDoesNotStopDeliveries(t *testing.T) {
	f := newFixture(t)
	f.proc.cfg.Throttle = &fakeThrottle{wait: time.Hour, err: errors.New("redis: connection refused")}

	f.process(t)
	if len(f.sender.requests) != 1 {
		t.Fatal("a broken limiter must fail open, not hold every delivery back")
	}
}

func TestProcess_TheThrottleOnlyUsesSlotsForDeliveriesThatCouldBeSent(t *testing.T) {
	t.Run("a disabled endpoint", func(t *testing.T) {
		f := newFixture(t)
		th := &fakeThrottle{wait: time.Hour}
		f.proc.cfg.Throttle = th
		f.setWork(func(w *delivery.Work) { w.EndpointActive = false })
		f.process(t)
		if len(th.calls) != 0 {
			t.Fatal("nothing is sent to a disabled endpoint, so it must not be throttled")
		}
	})
	t.Run("a retry that is not due yet", func(t *testing.T) {
		f := newFixture(t)
		th := &fakeThrottle{}
		f.proc.cfg.Throttle = th
		due := testNow.Add(time.Minute)
		f.setWork(func(w *delivery.Work) { w.NextAttemptAt = &due })
		f.process(t)
		if len(th.calls) != 0 {
			t.Fatal("a delivery put back because it is early must not burn a throttle slot")
		}
	})
	t.Run("a delivery that already finished", func(t *testing.T) {
		f := newFixture(t)
		th := &fakeThrottle{}
		f.proc.cfg.Throttle = th
		f.setWork(func(w *delivery.Work) { w.Status = delivery.StatusSucceeded })
		f.process(t)
		if len(th.calls) != 0 {
			t.Fatal("a duplicate job must not burn a throttle slot")
		}
	})
}

func TestProcess_WithoutAThrottleNothingIsLimited(t *testing.T) {
	f := newFixture(t) // newFixture configures no throttle
	f.process(t)
	if len(f.sender.requests) != 1 {
		t.Fatal("with no throttle configured, deliveries must go straight out")
	}
}

// fakeObserver records what the processor reported.
type fakeObserver struct {
	attempts  []observedAttempt
	throttled int
	disabled  []string
}

type observedAttempt struct {
	outcome string
	took    time.Duration
}

func (f *fakeObserver) AttemptFinished(outcome string, took time.Duration) {
	f.attempts = append(f.attempts, observedAttempt{outcome, took})
}
func (f *fakeObserver) Throttled()                { f.throttled++ }
func (f *fakeObserver) EndpointDisabled(r string) { f.disabled = append(f.disabled, r) }
