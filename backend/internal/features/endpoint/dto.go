package endpoint

import (
	"time"

	"github.com/samirthapa/webhook-platform/internal/shared/httpx"
)

type createEndpointRequest struct {
	URL         string   `json:"url"`
	Description string   `json:"description"`
	EventTypes  []string `json:"event_types"`
}

// updateEndpointRequest is a partial update (PATCH): omitted fields stay unchanged, so every
// field is a pointer to tell "absent" apart from "set to empty".
type updateEndpointRequest struct {
	URL         *string   `json:"url"`
	Description *string   `json:"description"`
	EventTypes  *[]string `json:"event_types"`
}

// endpointResponse is the public view of an endpoint. It never contains the signing secret.
type endpointResponse struct {
	ID             string     `json:"id"`
	URL            string     `json:"url"`
	Description    string     `json:"description"`
	EventTypes     []string   `json:"event_types"`
	Status         Status     `json:"status"`
	DisabledReason *string    `json:"disabled_reason"`
	DisabledAt     *time.Time `json:"disabled_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// endpointWithSecretResponse is returned only when a secret is created or rotated.
type endpointWithSecretResponse struct {
	endpointResponse
	Secret string `json:"secret"`
}

func toResponse(e Endpoint) endpointResponse {
	eventTypes := e.EventTypes
	if eventTypes == nil {
		eventTypes = []string{} // JSON [] rather than null
	}
	return endpointResponse{
		ID:             e.ID.String(),
		URL:            e.URL,
		Description:    e.Description,
		EventTypes:     eventTypes,
		Status:         e.Status,
		DisabledReason: e.DisabledReason,
		DisabledAt:     httpx.UTCPtr(e.DisabledAt),
		CreatedAt:      e.CreatedAt.UTC(),
		UpdatedAt:      e.UpdatedAt.UTC(),
	}
}

func toResponseWithSecret(e Endpoint) endpointWithSecretResponse {
	return endpointWithSecretResponse{endpointResponse: toResponse(e), Secret: e.Secret}
}
