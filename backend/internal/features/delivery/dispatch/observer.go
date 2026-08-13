package dispatch

import (
	"time"

	"github.com/samirthapa/tern/internal/features/delivery"
)

// The outcomes of an attempt, as reported to an Observer.
const (
	OutcomeSucceeded = "succeeded"
	OutcomeRetrying  = "retrying" // it failed and a retry is scheduled
	OutcomeFailed    = "failed"   // it failed for good
	OutcomeSkipped   = "skipped"  // no request was made: the endpoint is disabled or its secret unusable
)

// The reasons an endpoint is disabled by the processor, as reported to an Observer.
const (
	DisabledAuto = "auto" // too many failed attempts in a row
	DisabledGone = "gone" // the endpoint answered HTTP 410
)

// Observer is told what the processor does, so that it can be measured. It is optional (see
// ProcessorConfig.Observer) and implemented by the metrics package, keeping this package free of
// any monitoring library.
type Observer interface {
	// AttemptFinished reports an attempt that was written to the delivery log. responseTime is how
	// long the HTTP exchange took, or zero if no request was made.
	AttemptFinished(outcome string, responseTime time.Duration)
	// Throttled reports a delivery held back to protect its endpoint.
	Throttled()
	// EndpointDisabled reports that this processor disabled an endpoint.
	EndpointDisabled(reason string)
}

// noopObserver is used when no Observer is configured.
type noopObserver struct{}

func (noopObserver) AttemptFinished(string, time.Duration) {}
func (noopObserver) Throttled()                            {}
func (noopObserver) EndpointDisabled(string)               {}

// outcomeOf names what an attempt came to, for an Observer.
func outcomeOf(v verdict, res attemptResult) string {
	switch {
	case res.skipped:
		return OutcomeSkipped
	case v.outcome == delivery.StatusSucceeded:
		return OutcomeSucceeded
	case v.outcome == delivery.StatusPending:
		return OutcomeRetrying
	default:
		return OutcomeFailed
	}
}
