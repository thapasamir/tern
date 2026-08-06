package endpoint

import (
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

// DisabledReasonManual is recorded when a customer disables an endpoint themselves.
const DisabledReasonManual = "disabled manually"

// Endpoint is a customer's webhook destination.
type Endpoint struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	URL         string
	Description string
	// EventTypes restricts which events are delivered. Empty means all of them.
	EventTypes []string
	// Secret is the HMAC signing key. It is only ever exposed on create and rotate.
	Secret         string
	Status         Status
	DisabledReason *string
	DisabledAt     *time.Time
	// ConsecutiveFailures counts failed delivery attempts since the endpoint last succeeded or was
	// re-enabled. Reaching the configured limit disables the endpoint.
	ConsecutiveFailures int
	CreatedAt           time.Time
	UpdatedAt           time.Time
}
