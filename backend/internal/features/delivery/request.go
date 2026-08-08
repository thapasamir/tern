package delivery

import (
	"encoding/json"
	"fmt"
	"time"

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
func buildRequest(w Work, at time.Time) (SendRequest, error) {
	body, err := json.Marshal(envelope{
		ID:        w.EventID.String(),
		Type:      w.EventType,
		CreatedAt: w.EventCreatedAt.UTC(),
		Data:      w.Payload,
	})
	if err != nil {
		return SendRequest{}, fmt.Errorf("encode payload: %w", err)
	}

	header, err := signing.Headers(w.EndpointSecret, w.EventID.String(), at, body)
	if err != nil {
		return SendRequest{}, fmt.Errorf("sign payload: %w", err)
	}
	return SendRequest{URL: w.EndpointURL, Header: header, Body: body}, nil
}
