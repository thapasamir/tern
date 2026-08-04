package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/samirthapa/webhook-platform/internal/shared/signing"
)

const maxErrorLen = 500

var errEndpointDisabled = errors.New("endpoint is disabled")

// Processor makes the delivery attempts. The worker calls Process for every job it claims.
type Processor struct {
	repo   Repository
	sender Sender
	now    func() time.Time
	log    *slog.Logger
}

func NewProcessor(repo Repository, sender Sender) *Processor {
	return &Processor{repo: repo, sender: sender, now: time.Now, log: slog.Default()}
}

// Process makes one attempt for the delivery named by jobID and records the outcome.
//
// The returned error means "could not do the work, try the job again later": the worker then
// leaves the job un-acknowledged and the queue redelivers it. A delivery that fails because the
// customer's endpoint misbehaved is not an error here; that is a recorded outcome.
//
// Jobs are delivered at least once, so Process must tolerate seeing a delivery twice. It does:
// a delivery that is no longer pending is skipped, and RecordAttempt resolves a race between two
// workers. What can still happen is that a crash between sending and recording sends the
// request twice, which is why every request carries a stable webhook-id for the receiver to
// de-duplicate on.
func (p *Processor) Process(ctx context.Context, jobID string) error {
	id, err := uuid.Parse(jobID)
	if err != nil {
		p.log.Warn("dropping job with a malformed id", "job", jobID)
		return nil
	}

	work, err := p.repo.GetWork(ctx, id)
	if errors.Is(err, ErrNotFound) {
		p.log.Warn("dropping job for a delivery that no longer exists", "delivery_id", id)
		return nil
	}
	if err != nil {
		return fmt.Errorf("load delivery %s: %w", id, err)
	}
	if work.Status != StatusPending {
		return nil // duplicate job: this delivery was already completed
	}

	started := p.now()
	result := p.attempt(ctx, work, started)
	rec := newAttemptRecord(work.DeliveryID, result, started, p.now())

	recorded, err := p.repo.RecordAttempt(ctx, rec)
	if err != nil {
		return fmt.Errorf("record attempt for delivery %s: %w", id, err)
	}
	p.log.Info("delivery attempt finished",
		"delivery_id", id,
		"event_id", work.EventID,
		"outcome", rec.Outcome,
		"status_code", result.StatusCode,
		"duration_ms", rec.Duration.Milliseconds(),
		"recorded", recorded,
	)
	return nil
}

func (p *Processor) attempt(ctx context.Context, w Work, at time.Time) SendResult {
	if !w.EndpointActive {
		return SendResult{Err: errEndpointDisabled}
	}
	body, err := encodeEnvelope(w)
	if err != nil {
		return SendResult{Err: fmt.Errorf("encode payload: %w", err)}
	}
	header, err := signing.Headers(w.EndpointSecret, w.EventID.String(), at, body)
	if err != nil {
		return SendResult{Err: fmt.Errorf("sign payload: %w", err)}
	}
	return p.sender.Send(ctx, SendRequest{URL: w.EndpointURL, Header: header, Body: body})
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

// newAttemptRecord turns a send result into the outcome to persist. A delivery succeeds only on
// a 2xx response; everything else (including redirects) is a failure.
func newAttemptRecord(deliveryID uuid.UUID, res SendResult, started, finished time.Time) AttemptRecord {
	rec := AttemptRecord{
		DeliveryID: deliveryID,
		Duration:   res.Duration,
		StartedAt:  started,
		FinishedAt: finished,
	}

	switch {
	case res.Err != nil:
		rec.Outcome = StatusFailed
		rec.Error = ptr(truncate(sanitize(res.Err.Error()), maxErrorLen))
	case res.StatusCode >= 200 && res.StatusCode < 300:
		rec.Outcome = StatusSucceeded
	default:
		rec.Outcome = StatusFailed
		rec.Error = ptr(fmt.Sprintf("endpoint responded with HTTP %d", res.StatusCode))
	}

	if res.StatusCode != 0 {
		rec.StatusCode = ptr(res.StatusCode)
	}
	if res.Body != "" {
		rec.ResponseBody = ptr(res.Body)
	}
	return rec
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

func ptr[T any](v T) *T { return &v }
