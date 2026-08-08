package delivery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// noRetry is the zero time. Process returns it to say "this job is finished, do not run it again".
var noRetry time.Time

var errEndpointDisabled = errors.New("endpoint is disabled")

// ProcessorConfig holds the reliability policy of the processor.
type ProcessorConfig struct {
	Retry RetryPolicy
	// AutoDisableAfter is how many failed attempts in a row disable an endpoint.
	AutoDisableAfter int
}

// Processor makes the delivery attempts. The worker calls Process for every job it claims.
//
// The work of one attempt is split across the files of this package by concern:
//
//	processor.go  the sequence of steps (this file)
//	request.go    building the signed HTTP request
//	verdict.go    deciding what an attempt's result means: succeed, retry or give up
//	record.go     turning the outcome into the database record
//	health.go     reporting the outcome to the endpoint, which may disable it
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
// when the job must run again (noRetry if it is finished), or an error if the work could not be
// done right now (see worker.Processor).
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
	work, ok, err := p.loadWork(ctx, jobID)
	if err != nil || !ok {
		return noRetry, err
	}

	started := p.now()
	if due := work.NextAttemptAt; due != nil && started.Before(*due) {
		// The job came back before the retry is due, e.g. after its lease expired because the
		// worker could not reschedule it. Never retry early: put it back where it belongs.
		return *due, nil
	}

	result := p.attempt(ctx, work, started)
	finished := p.now()
	v := p.judge(work, result, finished)
	rec := newAttemptRecord(work.DeliveryID, result.SendResult, v, started, finished)

	recorded, err := p.repo.RecordAttempt(ctx, rec)
	if err != nil {
		return noRetry, fmt.Errorf("record attempt for delivery %s: %w", work.DeliveryID, err)
	}
	p.logAttempt(work, result, rec, recorded)
	if !recorded {
		return noRetry, nil // another worker got there first; it owns the follow-up
	}

	p.reportHealth(ctx, work, v)
	return v.retryAt, nil
}

// loadWork reads the delivery a job refers to. ok is false when there is nothing to do: the job is
// malformed, its delivery no longer exists, or the delivery already finished (a duplicate job).
// Those jobs must be acknowledged and dropped, not retried, so they are not errors. An error is
// returned only when the database could not be read, which is worth trying again later.
func (p *Processor) loadWork(ctx context.Context, jobID string) (work Work, ok bool, err error) {
	id, err := uuid.Parse(jobID)
	if err != nil {
		p.log.Warn("dropping job with a malformed id", "job", jobID)
		return Work{}, false, nil
	}

	work, err = p.repo.GetWork(ctx, id)
	switch {
	case errors.Is(err, ErrNotFound):
		p.log.Warn("dropping job for a delivery that no longer exists", "delivery_id", id)
		return Work{}, false, nil
	case err != nil:
		return Work{}, false, fmt.Errorf("load delivery %s: %w", id, err)
	}
	return work, work.Status == StatusPending, nil
}

// attemptResult is what an attempt produced.
type attemptResult struct {
	SendResult
	// skipped is set when no request was made, for a reason that retrying cannot fix: the
	// endpoint is disabled, or its secret is unusable.
	skipped bool
}

// skipped is the result of an attempt that could not be made, for the given reason.
func skipped(reason error) attemptResult {
	return attemptResult{SendResult: SendResult{Err: reason}, skipped: true}
}

// attempt sends the delivery's request, unless there is no point in doing so.
func (p *Processor) attempt(ctx context.Context, w Work, at time.Time) attemptResult {
	if !w.EndpointActive {
		return skipped(errEndpointDisabled)
	}
	req, err := buildRequest(w, at)
	if err != nil {
		return skipped(err)
	}
	return attemptResult{SendResult: p.sender.Send(ctx, req)}
}

func (p *Processor) logAttempt(w Work, res attemptResult, rec AttemptRecord, recorded bool) {
	p.log.Info("delivery attempt finished",
		"delivery_id", w.DeliveryID,
		"event_id", w.EventID,
		"attempt", w.AttemptCount+1,
		"outcome", rec.Outcome,
		"status_code", res.StatusCode,
		"duration_ms", rec.Duration.Milliseconds(),
		"retry_at", rec.RetryAt,
		"recorded", recorded,
	)
}
