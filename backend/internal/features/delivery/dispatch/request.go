package dispatch

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/samirthapa/tern/internal/features/delivery"
	"github.com/samirthapa/tern/internal/features/delivery/sender"
	"github.com/samirthapa/tern/internal/shared/signing"
)

// envelope is the JSON body a customer's endpoint receives.
type envelope struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	CreatedAt time.Time       `json:"created_at"`
	Data      json.RawMessage `json:"data"`
}

// buildRequest assembles the signed HTTP request for one attempt at time at. The signature covers
// the event id, the time of this attempt and the exact body bytes, so each attempt is signed anew.
func buildRequest(w delivery.Work, at time.Time) (sender.Request, error) {
	body, err := json.Marshal(envelope{
		ID:        w.EventID.String(),
		Type:      w.EventType,
		CreatedAt: w.EventCreatedAt.UTC(),
		Data:      w.Payload,
	})
	if err != nil {
		return sender.Request{}, fmt.Errorf("encode payload: %w", err)
	}

	header, err := signing.Headers(w.EndpointSecret, w.EventID.String(), at, body)
	if err != nil {
		return sender.Request{}, fmt.Errorf("sign payload: %w", err)
	}
	return sender.Request{URL: w.EndpointURL, Header: header, Body: body}, nil
}
