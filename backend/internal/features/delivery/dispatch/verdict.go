package dispatch

import (
	"net/http"
	"time"

	"github.com/samirthapa/tern/internal/features/delivery"
)

// healthAction is what an attempt tells the endpoint about its health.
type healthAction int

const (
	healthNone    healthAction = iota // nothing: the attempt says nothing about the endpoint
	healthSuccess                     // the endpoint answered fine
	healthFailure                     // the endpoint failed this attempt
	healthGone                        // the endpoint says it is gone for good (HTTP 410)
)

// verdict is what an attempt's result means, for the delivery and for the endpoint.
type verdict struct {
	outcome delivery.Status // succeeded, failed, or pending (a retry is scheduled)
	retryAt time.Time       // when to try again; set exactly when outcome is pending
	health  healthAction
}

// judge decides what an attempt's result means:
//   - a 2xx response succeeds;
//   - a 410 fails for good at once, because the endpoint says it will never be back;
//   - an attempt that could not be made at all fails for good, because trying again changes nothing;
//   - anything else (network errors, timeouts, 4xx, 5xx, redirects) is retried while the schedule
//     has attempts left, and fails for good after that.
func (p *Processor) judge(w delivery.Work, res attemptResult, now time.Time) verdict {
	switch {
	case res.skipped:
		return verdict{outcome: delivery.StatusFailed}
	case res.Err == nil && isSuccess(res.StatusCode):
		return verdict{outcome: delivery.StatusSucceeded, health: healthSuccess}
	case res.Err == nil && res.StatusCode == http.StatusGone:
		return verdict{outcome: delivery.StatusFailed, health: healthGone}
	}
	return p.retryOrGiveUp(w, res, now)
}

// retryOrGiveUp handles a failure that is worth retrying: it schedules the next attempt if the
// retry schedule has one left, and fails the delivery for good if not.
func (p *Processor) retryOrGiveUp(w delivery.Work, res attemptResult, now time.Time) verdict {
	var retryAfter time.Duration
	if honoursRetryAfter(res.StatusCode) {
		retryAfter = res.RetryAfter
	}

	delay, ok := p.cfg.Retry.NextDelay(w.AttemptCount+1, retryAfter)
	if !ok {
		return verdict{outcome: delivery.StatusFailed, health: healthFailure}
	}
	return verdict{outcome: delivery.StatusPending, retryAt: now.Add(delay), health: healthFailure}
}

func isSuccess(status int) bool { return status >= 200 && status < 300 }

// honoursRetryAfter reports whether a response with this status may postpone the retry:
// the two statuses that mean "too busy right now, ask again later".
func honoursRetryAfter(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable
}
