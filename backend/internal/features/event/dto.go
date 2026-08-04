package event

import (
	"encoding/json"
	"time"
)

type createEventRequest struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type eventResponse struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

func toResponse(e Event) eventResponse {
	return eventResponse{
		ID:        e.ID.String(),
		Type:      e.Type,
		Payload:   e.Payload,
		CreatedAt: e.CreatedAt.UTC(),
	}
}
