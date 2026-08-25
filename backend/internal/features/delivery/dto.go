package delivery

import (
	"time"

	"github.com/samirthapa/tern/internal/shared/httpx"
)

type deliveryResponse struct {
	ID           string `json:"id"`
	EventID      string `json:"event_id"`
	EndpointID   string `json:"endpoint_id"`
	Status       Status `json:"status"`
	AttemptCount int    `json:"attempt_count"`
	// ReplayedFrom is the delivery this one repeats, if it was created by a replay.
	ReplayedFrom *string `json:"replayed_from"`
	// NextAttemptAt is when the next retry is due, for a delivery that failed but will be retried.
	NextAttemptAt *time.Time `json:"next_attempt_at"`
	CreatedAt     time.Time  `json:"created_at"`
	CompletedAt   *time.Time `json:"completed_at"`
}

// deliveryViewResponse is a delivery in the log and on its own page: the delivery, plus what is
// needed to recognise it (event type, endpoint URL) and how its latest attempt went.
type deliveryViewResponse struct {
	deliveryResponse
	EventType   string `json:"event_type"`
	EndpointURL string `json:"endpoint_url"`
	// LastAttempt is null until the first attempt has finished.
	LastAttempt *lastAttemptResponse `json:"last_attempt"`
}

type lastAttemptResponse struct {
	StatusCode *int      `json:"status_code"`
	Error      *string   `json:"error"`
	DurationMs int64     `json:"duration_ms"`
	StartedAt  time.Time `json:"started_at"`
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
	deliveryViewResponse
	Attempts []attemptResponse `json:"attempts"`
}

// replayRequest selects the failed deliveries to replay: those created in [Since, Until).
type replayRequest struct {
	Since time.Time  `json:"since"`
	Until *time.Time `json:"until"` // optional: defaults to now
}

type replayResponse struct {
	Replayed int  `json:"replayed"`
	HasMore  bool `json:"has_more"`
}

func toResponse(d Delivery) deliveryResponse {
	var replayedFrom *string
	if d.ReplayedFrom != nil {
		id := d.ReplayedFrom.String()
		replayedFrom = &id
	}
	return deliveryResponse{
		ReplayedFrom:  replayedFrom,
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

func toViewResponse(v View) deliveryViewResponse {
	out := deliveryViewResponse{deliveryResponse: toResponse(v.Delivery), EventType: v.EventType, EndpointURL: v.EndpointURL}
	if v.LastAttempt != nil {
		out.LastAttempt = &lastAttemptResponse{
			StatusCode: v.LastAttempt.StatusCode,
			Error:      v.LastAttempt.Error,
			DurationMs: v.LastAttempt.Duration.Milliseconds(),
			StartedAt:  v.LastAttempt.StartedAt.UTC(),
		}
	}
	return out
}

func toDetailResponse(d View, attempts []Attempt) deliveryDetailResponse {
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
	return deliveryDetailResponse{deliveryViewResponse: toViewResponse(d), Attempts: out}
}
