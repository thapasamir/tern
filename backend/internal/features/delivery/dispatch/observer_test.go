package dispatch

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/samirthapa/tern/internal/features/delivery"
	"github.com/samirthapa/tern/internal/features/delivery/sender"
)

func TestObserver_ReportsEachKindOfAttempt(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(*processorFixture)
		want    string
	}{
		{"a 200", func(f *processorFixture) {}, OutcomeSucceeded},
		{"a 500 with retries left", func(f *processorFixture) { f.sender.result = sender.Result{StatusCode: 500} }, OutcomeRetrying},
		{"a network error with retries left", func(f *processorFixture) { f.sender.result = sender.Result{Err: errors.New("refused")} }, OutcomeRetrying},
		{"a 500 on the last attempt", func(f *processorFixture) {
			f.setWork(func(w *delivery.Work) { w.AttemptCount = 2 })
			f.sender.result = sender.Result{StatusCode: 500}
		}, OutcomeFailed},
		{"a 410", func(f *processorFixture) { f.sender.result = sender.Result{StatusCode: http.StatusGone} }, OutcomeFailed},
		{"a disabled endpoint", func(f *processorFixture) { f.setWork(func(w *delivery.Work) { w.EndpointActive = false }) }, OutcomeSkipped},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			obs := &fakeObserver{}
			f.proc.cfg.Observer = obs
			c.prepare(f)

			f.process(t)
			if len(obs.attempts) != 1 || obs.attempts[0].outcome != c.want {
				t.Fatalf("observed %+v, want exactly one %q attempt", obs.attempts, c.want)
			}
		})
	}
}

func TestObserver_ReportsTheResponseTime(t *testing.T) {
	f := newFixture(t)
	obs := &fakeObserver{}
	f.proc.cfg.Observer = obs
	f.sender.result = sender.Result{StatusCode: 200, Duration: 250 * time.Millisecond}
	f.process(t)
	if obs.attempts[0].took != 250*time.Millisecond {
		t.Fatalf("response time = %v, want 250ms", obs.attempts[0].took)
	}
}

func TestObserver_OnlyHearsAboutAttemptsThatWereRecorded(t *testing.T) {
	t.Run("a worker that lost the race", func(t *testing.T) {
		f := newFixture(t)
		obs := &fakeObserver{}
		f.proc.cfg.Observer = obs
		f.repo.notRecorded = true
		f.process(t)
		if len(obs.attempts) != 0 {
			t.Fatal("an attempt that was not recorded must not be counted: the other worker counts it")
		}
	})
	t.Run("a delivery that is not due yet", func(t *testing.T) {
		f := newFixture(t)
		obs := &fakeObserver{}
		f.proc.cfg.Observer = obs
		due := testNow.Add(time.Minute)
		f.setWork(func(w *delivery.Work) { w.NextAttemptAt = &due })
		f.process(t)
		if len(obs.attempts) != 0 {
			t.Fatal("no attempt was made")
		}
	})
}

func TestObserver_ReportsThrottling(t *testing.T) {
	f := newFixture(t)
	obs := &fakeObserver{}
	f.proc.cfg.Observer = obs
	f.proc.cfg.Throttle = &fakeThrottle{wait: time.Second}
	f.process(t)
	if obs.throttled != 1 || len(obs.attempts) != 0 {
		t.Fatalf("throttled %d times with %d attempts; want 1 and 0 (a throttled delivery is not an attempt)", obs.throttled, len(obs.attempts))
	}
}

func TestObserver_ReportsWhyAnEndpointWasDisabled(t *testing.T) {
	t.Run("too many failures", func(t *testing.T) {
		f := newFixture(t)
		obs := &fakeObserver{}
		f.proc.cfg.Observer = obs
		f.sender.result = sender.Result{StatusCode: 500}
		f.health.disableOnFailure = true
		f.process(t)
		if len(obs.disabled) != 1 || obs.disabled[0] != DisabledAuto {
			t.Fatalf("disabled = %v, want [%s]", obs.disabled, DisabledAuto)
		}
	})
	t.Run("HTTP 410", func(t *testing.T) {
		f := newFixture(t)
		obs := &fakeObserver{}
		f.proc.cfg.Observer = obs
		f.sender.result = sender.Result{StatusCode: http.StatusGone}
		f.health.disableOnDisable = true
		f.process(t)
		if len(obs.disabled) != 1 || obs.disabled[0] != DisabledGone {
			t.Fatalf("disabled = %v, want [%s]", obs.disabled, DisabledGone)
		}
	})
	t.Run("an endpoint that was already disabled", func(t *testing.T) {
		f := newFixture(t)
		obs := &fakeObserver{}
		f.proc.cfg.Observer = obs
		f.sender.result = sender.Result{StatusCode: 500} // fakeHealth says nothing was newly disabled
		f.process(t)
		if len(obs.disabled) != 0 {
			t.Fatal("only the call that actually disabled the endpoint may be reported")
		}
	})
}

func TestObserver_IsOptional(t *testing.T) {
	f := newFixture(t) // newFixture configures no observer
	f.process(t)       // must not panic
}
