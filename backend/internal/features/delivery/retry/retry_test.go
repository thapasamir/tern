package retry

import (
	"testing"
	"time"
)

// fixedRand returns a policy whose jitter source always yields r.
func fixedRand(p Policy, r float64) Policy {
	p.rand = func() float64 { return r }
	return p
}

func TestPolicy_FollowsTheScheduleAndThenStops(t *testing.T) {
	p := fixedRand(NewPolicy([]time.Duration{5 * time.Second, 5 * time.Minute, time.Hour}), 0.5) // 0.5 = no jitter

	want := []time.Duration{5 * time.Second, 5 * time.Minute, time.Hour}
	for i, wantDelay := range want {
		attempt := i + 1
		delay, ok := p.NextDelay(attempt, 0)
		if !ok || delay != wantDelay {
			t.Errorf("after failed attempt %d: delay=%v ok=%v, want %v", attempt, delay, ok, wantDelay)
		}
	}
	if _, ok := p.NextDelay(4, 0); ok {
		t.Error("attempt 4 failed but the schedule is exhausted: there must be no retry")
	}
	if p.MaxAttempts() != 4 {
		t.Errorf("MaxAttempts = %d, want 4 (the first try plus 3 retries)", p.MaxAttempts())
	}
}

func TestPolicy_NeverRetriesWithAnEmptySchedule(t *testing.T) {
	p := NewPolicy(nil)
	if _, ok := p.NextDelay(1, 0); ok {
		t.Fatal("an empty schedule must mean no retries")
	}
	if p.MaxAttempts() != 1 {
		t.Fatalf("MaxAttempts = %d, want 1", p.MaxAttempts())
	}
	if _, ok := (Policy{}).NextDelay(1, 0); ok {
		t.Fatal("the zero policy must not retry (and must not panic)")
	}
}

func TestPolicy_RejectsNonsenseAttemptNumbers(t *testing.T) {
	p := NewPolicy([]time.Duration{time.Second})
	for _, attempt := range []int{0, -1} {
		if _, ok := p.NextDelay(attempt, 0); ok {
			t.Errorf("NextDelay(%d) must not allow a retry", attempt)
		}
	}
}

func TestPolicy_JitterStaysWithinItsBounds(t *testing.T) {
	base := 10 * time.Minute
	p := NewPolicy([]time.Duration{base})

	lowest, _ := fixedRand(p, 0).NextDelay(1, 0)
	middle, _ := fixedRand(p, 0.5).NextDelay(1, 0)
	highest, _ := fixedRand(p, 0.999999).NextDelay(1, 0)

	if lowest != 8*time.Minute {
		t.Errorf("lowest jitter = %v, want 80%% of %v", lowest, base)
	}
	if middle != base {
		t.Errorf("middle jitter = %v, want exactly %v", middle, base)
	}
	if highest < 11*time.Minute+50*time.Second || highest > 12*time.Minute {
		t.Errorf("highest jitter = %v, want just under 120%% of %v", highest, base)
	}
}

func TestPolicy_RealRandomnessStaysInRange(t *testing.T) {
	p := NewPolicy([]time.Duration{time.Minute})
	seen := map[time.Duration]bool{}
	for i := 0; i < 200; i++ {
		d, _ := p.NextDelay(1, 0)
		if d < 48*time.Second || d > 72*time.Second {
			t.Fatalf("delay %v is outside the +-20%% band around 1m", d)
		}
		seen[d] = true
	}
	if len(seen) < 20 {
		t.Fatalf("only %d distinct delays in 200 draws: the jitter is not random", len(seen))
	}
}

func TestPolicy_HonoursRetryAfter(t *testing.T) {
	p := fixedRand(NewPolicy([]time.Duration{time.Minute}), 0.5)

	cases := []struct {
		name       string
		retryAfter time.Duration
		want       time.Duration
	}{
		{"absent", 0, time.Minute},
		{"asks for longer than our backoff", 10 * time.Minute, 10 * time.Minute},
		{"asks for less than our backoff: ignored, we do not hammer", 5 * time.Second, time.Minute},
		{"absurdly long: capped", 100 * time.Hour, DefaultMaxRetryAfter},
		{"negative: ignored", -time.Hour, time.Minute},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := p.NextDelay(1, c.retryAfter)
			if !ok || got != c.want {
				t.Fatalf("NextDelay = %v, %v; want %v", got, ok, c.want)
			}
		})
	}
}

func TestPolicy_DoesNotShareItsScheduleWithTheCaller(t *testing.T) {
	schedule := []time.Duration{time.Minute}
	p := fixedRand(NewPolicy(schedule), 0.5)
	schedule[0] = time.Hour // mutating the caller's slice afterwards must not change the policy

	if d, _ := p.NextDelay(1, 0); d != time.Minute {
		t.Fatalf("delay = %v, the policy must keep its own copy of the schedule", d)
	}
}

func TestPolicy_WithJitterZeroGivesExactDelays(t *testing.T) {
	p := NewPolicy([]time.Duration{time.Minute}).WithJitter(0)
	for i := 0; i < 50; i++ { // real randomness underneath: zero jitter must still be exact
		if d, _ := p.NextDelay(1, 0); d != time.Minute {
			t.Fatalf("delay = %v, want exactly 1m with no jitter", d)
		}
	}
}

func TestPolicy_WithJitterDoesNotChangeTheOriginal(t *testing.T) {
	original := NewPolicy([]time.Duration{time.Minute})
	_ = original.WithJitter(0)
	seen := map[time.Duration]bool{}
	for i := 0; i < 50; i++ {
		d, _ := original.NextDelay(1, 0)
		seen[d] = true
	}
	if len(seen) < 5 {
		t.Fatal("WithJitter must return a copy and leave the original's jitter alone")
	}
}
