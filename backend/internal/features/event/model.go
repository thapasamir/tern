package event

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Event is something that happened in the customer's system, to be delivered to their endpoints.
type Event struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	// Type is a dotted name such as "invoice.paid"; endpoints subscribe to types.
	Type    string
	Payload json.RawMessage
	// IdempotencyKey, when set, makes retries of the same send return the original event.
	IdempotencyKey *string
	CreatedAt      time.Time
}
