package delivery

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// QueueName is the Redis namespace of the delivery job queue, shared by the API and the worker.
const QueueName = "tern:deliveries"

type Status string

const (
	// StatusPending: no attempt has finished yet. The job is queued or waiting to be queued.
	StatusPending   Status = "pending"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

// Delivery is one event on its way to one endpoint.
type Delivery struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	EventID      uuid.UUID
	EndpointID   uuid.UUID
	Status       Status
	AttemptCount int
	// NextAttemptAt is when a failed delivery is due for its next retry; nil otherwise.
	NextAttemptAt *time.Time
	EnqueuedAt    *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	CompletedAt   *time.Time
}

// Attempt is the record of one HTTP request made for a delivery.
type Attempt struct {
	ID         uuid.UUID
	DeliveryID uuid.UUID
	Number     int
	// StatusCode is nil when no HTTP response was received (DNS failure, timeout, ...).
	StatusCode *int
	Error      *string
	// ResponseBody is the beginning of what the endpoint answered, kept for debugging.
	ResponseBody *string
	Duration     time.Duration
	StartedAt    time.Time
}

// Work is everything the worker needs to make one attempt, loaded in a single query.
type Work struct {
	DeliveryID uuid.UUID
	TenantID   uuid.UUID
	EndpointID uuid.UUID
	Status     Status
	// AttemptCount is how many attempts have been made so far; the next one is AttemptCount+1.
	AttemptCount int
	// NextAttemptAt is when the delivery is due; nil for one that has not been retried yet.
	NextAttemptAt  *time.Time
	EventID        uuid.UUID
	EventType      string
	Payload        json.RawMessage
	EventCreatedAt time.Time
	EndpointURL    string
	EndpointSecret string
	EndpointActive bool
}

// AttemptRecord is the outcome of an attempt, to be persisted.
type AttemptRecord struct {
	DeliveryID uuid.UUID
	// Outcome is the delivery's status after this attempt: StatusPending when it failed but will
	// be retried (RetryAt is then set), otherwise the final StatusSucceeded or StatusFailed.
	Outcome Status
	// RetryAt is when the next attempt is due. Set if and only if Outcome is StatusPending.
	RetryAt      *time.Time
	StatusCode   *int
	Error        *string
	ResponseBody *string
	Duration     time.Duration
	StartedAt    time.Time
	FinishedAt   time.Time
}
