package delivery

import (
	"net/http"

	"github.com/samirthapa/webhook-platform/internal/shared/httpx"
	"github.com/samirthapa/webhook-platform/internal/shared/pagination"
)

// Controller translates HTTP requests into service calls. It owns parsing and response
// shaping only; business rules live in Service.
type Controller struct {
	svc *Service
}

func NewController(svc *Service) *Controller { return &Controller{svc: svc} }

func (c *Controller) listByEvent(w http.ResponseWriter, r *http.Request) error {
	tenantID, err := httpx.RequireTenantID(r)
	if err != nil {
		return err
	}
	eventID, err := httpx.PathUUID(r, "eventID", "event id")
	if err != nil {
		return err
	}
	params, err := pagination.Parse(r)
	if err != nil {
		return err
	}
	res, err := c.svc.ListByEvent(r.Context(), tenantID, eventID, params)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, pagination.ToPage(res, toResponse))
	return nil
}

func (c *Controller) get(w http.ResponseWriter, r *http.Request) error {
	tenantID, err := httpx.RequireTenantID(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathUUID(r, "deliveryID", "delivery id")
	if err != nil {
		return err
	}
	d, attempts, err := c.svc.GetWithAttempts(r.Context(), tenantID, id)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, toDetailResponse(d, attempts))
	return nil
}
