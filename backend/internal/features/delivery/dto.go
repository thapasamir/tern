package delivery

import (
	"time"

	"github.com/samirthapa/webhook-platform/internal/shared/httpx"
)

type deliveryResponse struct {
	ID           string `json:"id"`
	EventID      string `json:"event_id"`
	EndpointID   string `json:"endpoint_id"`
	Status       Status `json:"status"`
	AttemptCount int    `json:"attempt_count"`
	// NextAttemptAt is when the next retry is due, for a delivery that failed but will be retried.
	NextAttemptAt *time.Time `json:"next_attempt_at"`
	CreatedAt     time.Time  `json:"created_at"`
	CompletedAt   *time.Time `json:"completed_at"`
}

type attemptResponse struct {
	AttemptNumber int       `json:"attempt_number"`
	StatusCode    *int      `json:"status_code"`
	Error         *string   `json:"error"`
	ResponseBody  *string   `json:"response_body"`
	DurationMs    int64     `json:"duration_ms"`
	StartedAt     time.Time `json:"started_at"`
}

type deliveryDetailResponse struct {
	deliveryResponse
	Attempts []attemptResponse `json:"attempts"`
}

func toResponse(d Delivery) deliveryResponse {
	return deliveryResponse{
		ID:            d.ID.String(),
		EventID:       d.EventID.String(),
		EndpointID:    d.EndpointID.String(),
		Status:        d.Status,
		AttemptCount:  d.AttemptCount,
		NextAttemptAt: httpx.UTCPtr(d.NextAttemptAt),
		CreatedAt:     d.CreatedAt.UTC(),
		CompletedAt:   httpx.UTCPtr(d.CompletedAt),
	}
}

func toDetailResponse(d Delivery, attempts []Attempt) deliveryDetailResponse {
	out := make([]attemptResponse, 0, len(attempts)) // JSON [] rather than null
	for _, a := range attempts {
		out = append(out, attemptResponse{
			AttemptNumber: a.Number,
			StatusCode:    a.StatusCode,
			Error:         a.Error,
			ResponseBody:  a.ResponseBody,
			DurationMs:    a.Duration.Milliseconds(),
			StartedAt:     a.StartedAt.UTC(),
		})
	}
	return deliveryDetailResponse{deliveryResponse: toResponse(d), Attempts: out}
}
