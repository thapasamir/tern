package delivery

import "time"

type deliveryResponse struct {
	ID           string     `json:"id"`
	EventID      string     `json:"event_id"`
	EndpointID   string     `json:"endpoint_id"`
	Status       Status     `json:"status"`
	AttemptCount int        `json:"attempt_count"`
	CreatedAt    time.Time  `json:"created_at"`
	CompletedAt  *time.Time `json:"completed_at"`
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
		ID:           d.ID.String(),
		EventID:      d.EventID.String(),
		EndpointID:   d.EndpointID.String(),
		Status:       d.Status,
		AttemptCount: d.AttemptCount,
		CreatedAt:    d.CreatedAt.UTC(),
		CompletedAt:  utcPtr(d.CompletedAt),
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

// utcPtr keeps API timestamps in one timezone regardless of what the database driver returns.
func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
