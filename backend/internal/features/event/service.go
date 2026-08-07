package event

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/shared/apperror"
	"github.com/samirthapa/tern/internal/shared/eventtype"
	"github.com/samirthapa/tern/internal/shared/ids"
)

const (
	maxPayloadBytes      = 256 * 1024
	maxIdempotencyKeyLen = 255
	enqueueTimeout       = 5 * time.Second
)

// Enqueuer schedules deliveries for execution. It is implemented by the delivery feature and
// wired in main.go, so this feature knows nothing about Redis or workers.
type Enqueuer interface {
	Enqueue(ctx context.Context, deliveryIDs []uuid.UUID) error
}

// IngestInput is a send request as received from the API client.
type IngestInput struct {
	Type           string
	Payload        json.RawMessage
	IdempotencyKey string // optional
}

// IngestResult is the outcome of Ingest.
type IngestResult struct {
	Event Event
	// Created is false when the request was an idempotent replay of an earlier one.
	Created bool
}

// Service holds the business rules for events.
type Service struct {
	repo     Repository
	enqueuer Enqueuer
	now      func() time.Time
}

func NewService(repo Repository, enqueuer Enqueuer) *Service {
	return &Service{repo: repo, enqueuer: enqueuer, now: time.Now}
}

// Ingest durably records an event and schedules its deliveries.
//
// Once the transaction commits the event is accepted, whatever happens next: a failure to reach
// the queue is logged but not returned, because the delivery rows exist and the worker's outbox
// sweeper will enqueue them shortly. Failing the request would make clients retry and re-send
// an event we already stored.
func (s *Service) Ingest(ctx context.Context, tenantID uuid.UUID, in IngestInput) (IngestResult, error) {
	if err := validateType(in.Type); err != nil {
		return IngestResult{}, err
	}
	if err := validatePayload(in.Payload); err != nil {
		return IngestResult{}, err
	}
	key, err := validateIdempotencyKey(in.IdempotencyKey)
	if err != nil {
		return IngestResult{}, err
	}

	e := Event{
		ID:             ids.New(),
		TenantID:       tenantID,
		Type:           in.Type,
		Payload:        in.Payload,
		IdempotencyKey: key,
		CreatedAt:      s.now().UTC(),
	}
	res, err := s.repo.Create(ctx, e)
	if err != nil {
		return IngestResult{}, apperror.Internal(err)
	}

	if !res.Created {
		existing, err := s.repo.GetByIdempotencyKey(ctx, tenantID, *key)
		if err != nil {
			return IngestResult{}, mapRepoError(err)
		}
		return IngestResult{Event: existing}, nil
	}

	s.enqueue(ctx, e.ID, res.DeliveryIDs)
	return IngestResult{Event: e, Created: true}, nil
}

func (s *Service) Get(ctx context.Context, tenantID, id uuid.UUID) (Event, error) {
	e, err := s.repo.Get(ctx, tenantID, id)
	if err != nil {
		return Event{}, mapRepoError(err)
	}
	return e, nil
}

func (s *Service) enqueue(ctx context.Context, eventID uuid.UUID, deliveryIDs []uuid.UUID) {
	if len(deliveryIDs) == 0 {
		return
	}
	// The client may hang up right after we commit; that must not abort the enqueue.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), enqueueTimeout)
	defer cancel()
	if err := s.enqueuer.Enqueue(ctx, deliveryIDs); err != nil {
		slog.Error("enqueue after commit failed; the outbox sweeper will retry",
			"event_id", eventID, "deliveries", len(deliveryIDs), "error", err)
	}
}

func validateType(t string) error {
	if t == "" {
		return apperror.Invalid("type is required")
	}
	if !eventtype.Valid(t) {
		return apperror.Invalid(fmt.Sprintf("invalid event type %q: %s", t, eventtype.Hint))
	}
	return nil
}

// validatePayload requires a JSON object: customers can always add fields to an object later
// without breaking consumers, which is not true of bare strings, numbers or arrays.
func validatePayload(p json.RawMessage) error {
	trimmed := bytes.TrimSpace(p)
	if len(trimmed) == 0 {
		return apperror.Invalid("payload is required")
	}
	if trimmed[0] != '{' {
		return apperror.Invalid("payload must be a JSON object")
	}
	if len(p) > maxPayloadBytes {
		return apperror.Invalid(fmt.Sprintf("payload must be at most %d KiB", maxPayloadBytes/1024))
	}
	return nil
}

// validateIdempotencyKey returns nil for an empty key (the header is optional).
func validateIdempotencyKey(k string) (*string, error) {
	if k == "" {
		return nil, nil
	}
	if len(k) > maxIdempotencyKeyLen {
		return nil, apperror.Invalid(fmt.Sprintf("Idempotency-Key must be at most %d characters", maxIdempotencyKeyLen))
	}
	for _, r := range k {
		if r < 0x20 || r > 0x7e {
			return nil, apperror.Invalid("Idempotency-Key must contain printable ASCII characters only")
		}
	}
	return &k, nil
}

func mapRepoError(err error) error { return apperror.FromStore(err, "event not found") }
