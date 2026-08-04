package event

import (
	"net/http"

	"github.com/samirthapa/webhook-platform/internal/shared/httpx"
)

const (
	idempotencyKeyHeader     = "Idempotency-Key"
	idempotentReplayedHeader = "Idempotent-Replayed"
)

// Controller translates HTTP requests into service calls. It owns parsing and response
// shaping only; business rules live in Service.
type Controller struct {
	svc *Service
}

func NewController(svc *Service) *Controller { return &Controller{svc: svc} }

// create accepts an event. Delivery is asynchronous, so a new event answers 202 Accepted;
// a replay of an earlier request (same Idempotency-Key) answers 200 with the original event.
func (c *Controller) create(w http.ResponseWriter, r *http.Request) error {
	tenantID, err := httpx.RequireTenantID(r)
	if err != nil {
		return err
	}
	var req createEventRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	res, err := c.svc.Ingest(r.Context(), tenantID, IngestInput{
		Type:           req.Type,
		Payload:        req.Payload,
		IdempotencyKey: r.Header.Get(idempotencyKeyHeader),
	})
	if err != nil {
		return err
	}

	status := http.StatusAccepted
	if !res.Created {
		status = http.StatusOK
		w.Header().Set(idempotentReplayedHeader, "true")
	}
	httpx.JSON(w, status, toResponse(res.Event))
	return nil
}

func (c *Controller) get(w http.ResponseWriter, r *http.Request) error {
	tenantID, err := httpx.RequireTenantID(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathUUID(r, "eventID", "event id")
	if err != nil {
		return err
	}
	e, err := c.svc.Get(r.Context(), tenantID, id)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, toResponse(e))
	return nil
}
