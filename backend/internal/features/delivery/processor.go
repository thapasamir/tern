package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/shared/ptr"
	"github.com/samirthapa/tern/internal/shared/signing"
)

const (
	maxErrorLen = 500

	// reasonGone is recorded when an endpoint is disabled because it answered 410 Gone.
	reasonGone = "endpoint responded with HTTP 410 Gone"
)

var errEndpointDisabled = errors.New("endpoint is disabled")

// EndpointHealth lets the processor report how an endpoint is doing, without knowing how the
// endpoint feature stores or acts on it. It is implemented by endpoint.Service and wired in main.go.
type EndpointHealth interface {
	// RecordSuccess clears the endpoint's streak of failures.
	RecordSuccess(ctx context.Context, tenantID, endpointID uuid.UUID) error
	// RecordFailure counts a failed attempt and disables the endpoint after disableAfter in a row.
	// disabled is true only for the call that actually disabled it.
	RecordFailure(ctx context.Context, tenantID, endpointID uuid.UUID, disableAfter int) (disabled bool, err error)
	// DisableIfActive disables the endpoint at once, unless it already is. disabled is true only
	// for the call that actually disabled it.
	DisableIfActive(ctx context.Context, tenantID, endpointID uuid.UUID, reason string) (disabled bool, err error)
}

// ProcessorConfig holds the reliability policy of the processor.
type ProcessorConfig struct {
	Retry RetryPolicy
	// AutoDisableAfter is how many failed attempts in a row disable an endpoint.
	AutoDisableAfter int
}

// Processor makes the delivery attempts. The worker calls Process for every job it claims.
type Processor struct {
	repo   Repository
	sender Sender
	health EndpointHealth
	cfg    ProcessorConfig
	now    func() time.Time
	log    *slog.Logger
}

func NewProcessor(repo Repository, sender Sender, health EndpointHealth, cfg ProcessorConfig) *Processor {
	return &Processor{repo: repo, sender: sender, health: health, cfg: cfg, now: time.Now, log: slog.Default()}
}

// Process makes one attempt for the delivery named by jobID and records the outcome. It returns
// when the job must run again (the zero time if it is finished), or an error if the work could not
// be done right now (see worker.Processor).
//
// A delivery that fails because the customer's endpoint misbehaved is not an error here: that is
// a recorded outcome, and, unless the retries are used up, a retry is scheduled for it.
//
// Jobs are delivered at least once, so Process must tolerate seeing a delivery twice, and does:
// a delivery that is no longer pending is skipped, one that is not due yet is put back until it
// is, and RecordAttempt resolves a race between two workers. What can still happen is that a
// crash between sending and recording sends the request twice, which is why every request
// carries a stable webhook-id for the receiver to de-duplicate on.
func (p *Processor) Process(ctx context.Context, jobID string) (time.Time, error) {
	id, err := uuid.Parse(jobID)
	if err != nil {
		p.log.Warn("dropping job with a malformed id", "job", jobID)
		return time.Time{}, nil
	}

	work, err := p.repo.GetWork(ctx, id)
	if errors.Is(err, ErrNotFound) {
		p.log.Warn("dropping job for a delivery that no longer exists", "delivery_id", id)
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("load delivery %s: %w", id, err)
	}
	if work.Status != StatusPending {
		return time.Time{}, nil // duplicate job: this delivery was already completed
	}

	started := p.now()
	if work.NextAttemptAt != nil && started.Before(*work.NextAttemptAt) {
		// The job came back before the retry is due, e.g. after its lease expired because the
		// worker could not reschedule it. Never retry early: put it back where it belongs.
		return *work.NextAttemptAt, nil
	}

	result := p.attempt(ctx, work, started)
	v := p.judge(work, result, p.now())
	rec := newAttemptRecord(work.DeliveryID, result.SendResult, v, started, p.now())

	recorded, err := p.repo.RecordAttempt(ctx, rec)
	if err != nil {
		return time.Time{}, fmt.Errorf("record attempt for delivery %s: %w", id, err)
	}
	p.log.Info("delivery attempt finished",
		"delivery_id", id,
		"event_id", work.EventID,
		"attempt", work.AttemptCount+1,
		"outcome", rec.Outcome,
		"status_code", result.StatusCode,
		"duration_ms", rec.Duration.Milliseconds(),
		"retry_at", rec.RetryAt,
		"recorded", recorded,
	)
	if !recorded {
		return time.Time{}, nil // another worker got there first; it owns the follow-up
	}

	p.updateHealth(ctx, work, v)
	if v.retryAt != nil {
		return *v.retryAt, nil
	}
	return time.Time{}, nil
}

// attemptResult is what an attempt produced.
type attemptResult struct {
	SendResult
	// skipped is set when no request was made, for a reason that retrying cannot fix: the
	// endpoint is disabled, or its secret is unusable.
	skipped bool
}

func (p *Processor) attempt(ctx context.Context, w Work, at time.Time) attemptResult {
	if !w.EndpointActive {
		return attemptResult{SendResult: SendResult{Err: errEndpointDisabled}, skipped: true}
	}
	body, err := encodeEnvelope(w)
	if err != nil {
		return attemptResult{SendResult: SendResult{Err: fmt.Errorf("encode payload: %w", err)}, skipped: true}
	}
	header, err := signing.Headers(w.EndpointSecret, w.EventID.String(), at, body)
	if err != nil {
		return attemptResult{SendResult: SendResult{Err: fmt.Errorf("sign payload: %w", err)}, skipped: true}
	}
	return attemptResult{SendResult: p.sender.Send(ctx, SendRequest{URL: w.EndpointURL, Header: header, Body: body})}
}

// healthAction is what an attempt tells the endpoint about its health.
type healthAction int

const (
	healthNone    healthAction = iota // nothing: the attempt says nothing about the endpoint
	healthSuccess                     // the endpoint answered fine
	healthFailure                     // the endpoint failed this attempt
	healthGone                        // the endpoint says it is gone for good (HTTP 410)
)

// verdict is the decision taken about an attempt.
type verdict struct {
	outcome Status     // succeeded, failed, or pending (a retry is scheduled)
	retryAt *time.Time // set exactly when outcome is pending
	health  healthAction
}

// judge decides what an attempt's result means:
//   - a 2xx response succeeds;
//   - a 410 fails for good at once, because the endpoint says it will never be back;
//   - an attempt that could not be made at all fails for good, because trying again changes nothing;
//   - anything else (network errors, timeouts, 4xx, 5xx, redirects) is retried while the schedule
//     has attempts left, and fails for good after that.
func (p *Processor) judge(w Work, res attemptResult, now time.Time) verdict {
	switch {
	case res.skipped:
		return verdict{outcome: StatusFailed, health: healthNone}
	case res.Err == nil && isSuccess(res.StatusCode):
		return verdict{outcome: StatusSucceeded, health: healthSuccess}
	case res.Err == nil && res.StatusCode == http.StatusGone:
		return verdict{outcome: StatusFailed, health: healthGone}
	}

	var retryAfter time.Duration
	if honoursRetryAfter(res.StatusCode) {
		retryAfter = res.RetryAfter
	}
	if delay, ok := p.cfg.Retry.NextDelay(w.AttemptCount+1, retryAfter); ok {
		at := now.Add(delay)
		return verdict{outcome: StatusPending, retryAt: &at, health: healthFailure}
	}
	return verdict{outcome: StatusFailed, health: healthFailure}
}

// updateHealth tells the endpoint how the attempt went. It runs after the attempt is recorded and
// is best effort: failing to update a counter must not fail the job and trigger a second send.
// When the update disables the endpoint, its other pending deliveries are failed on the spot
// rather than left to retry against an endpoint that will refuse them.
func (p *Processor) updateHealth(ctx context.Context, w Work, v verdict) {
	var (
		disabled bool
		err      error
	)
	switch v.health {
	case healthSuccess:
		err = p.health.RecordSuccess(ctx, w.TenantID, w.EndpointID)
	case healthFailure:
		disabled, err = p.health.RecordFailure(ctx, w.TenantID, w.EndpointID, p.cfg.AutoDisableAfter)
	case healthGone:
		disabled, err = p.health.DisableIfActive(ctx, w.TenantID, w.EndpointID, reasonGone)
	default:
		return
	}
	if err != nil {
		p.log.Error("updating endpoint health failed", "endpoint_id", w.EndpointID, "error", err)
		return
	}
	if !disabled {
		return
	}

	failed, err := p.repo.FailPendingForEndpoint(ctx, w.EndpointID, errEndpointDisabled.Error(), p.now())
	if err != nil {
		p.log.Error("failing the pending deliveries of a disabled endpoint failed",
			"endpoint_id", w.EndpointID, "error", err)
		return
	}
	p.log.Warn("endpoint disabled", "endpoint_id", w.EndpointID, "pending_deliveries_failed", failed)
}

// envelope is the JSON body a customer's endpoint receives.
type envelope struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	CreatedAt time.Time       `json:"created_at"`
	Data      json.RawMessage `json:"data"`
}

func encodeEnvelope(w Work) ([]byte, error) {
	return json.Marshal(envelope{
		ID:        w.EventID.String(),
		Type:      w.EventType,
		CreatedAt: w.EventCreatedAt.UTC(),
		Data:      w.Payload,
	})
}

// newAttemptRecord turns a send result and the verdict on it into the record to persist.
func newAttemptRecord(deliveryID uuid.UUID, res SendResult, v verdict, started, finished time.Time) AttemptRecord {
	rec := AttemptRecord{
		DeliveryID: deliveryID,
		Outcome:    v.outcome,
		RetryAt:    v.retryAt,
		Duration:   res.Duration,
		StartedAt:  started,
		FinishedAt: finished,
	}

	switch {
	case res.Err != nil:
		rec.Error = ptr.To(truncate(sanitize(res.Err.Error()), maxErrorLen))
	case !isSuccess(res.StatusCode):
		rec.Error = ptr.To(fmt.Sprintf("endpoint responded with HTTP %d", res.StatusCode))
	}
	if res.StatusCode != 0 {
		rec.StatusCode = ptr.To(res.StatusCode)
	}
	if res.Body != "" {
		rec.ResponseBody = ptr.To(res.Body)
	}
	return rec
}

func isSuccess(status int) bool { return status >= 200 && status < 300 }

// honoursRetryAfter reports whether a response with this status may postpone the retry:
// the two statuses that mean "too busy right now, ask again later".
func honoursRetryAfter(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable
}

// truncate shortens s to at most max bytes without splitting a UTF-8 character.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}
