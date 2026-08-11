package delivery

import (
	"net/http"
	"time"

	"github.com/samirthapa/tern/internal/shared/httpx"
	"github.com/samirthapa/tern/internal/shared/pagination"
)

// Controller translates HTTP requests into service calls. It owns parsing and response
// shaping only; business rules live in Service.
type Controller struct {
	svc *Service
}

func NewController(svc *Service) *Controller { return &Controller{svc: svc} }

// list is the delivery log: GET /deliveries?endpoint_id=&event_id=&status=&since=&until=
func (c *Controller) list(w http.ResponseWriter, r *http.Request) error {
	filter, err := filterFromQuery(r)
	if err != nil {
		return err
	}
	return c.respondWithList(w, r, filter)
}

// listByEvent is the delivery log narrowed to one event: GET /events/{eventID}/deliveries
func (c *Controller) listByEvent(w http.ResponseWriter, r *http.Request) error {
	eventID, err := httpx.PathUUID(r, "eventID", "event id")
	if err != nil {
		return err
	}
	return c.respondWithList(w, r, ListFilter{EventID: &eventID})
}

func (c *Controller) respondWithList(w http.ResponseWriter, r *http.Request, filter ListFilter) error {
	tenantID, err := httpx.RequireTenantID(r)
	if err != nil {
		return err
	}
	params, err := pagination.Parse(r)
	if err != nil {
		return err
	}
	res, err := c.svc.List(r.Context(), tenantID, filter, params)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, pagination.ToPage(res, toResponse))
	return nil
}

// filterFromQuery reads the optional log filters from the query string.
func filterFromQuery(r *http.Request) (ListFilter, error) {
	var (
		f   ListFilter
		err error
	)
	if f.EndpointID, err = httpx.QueryUUID(r, "endpoint_id"); err != nil {
		return ListFilter{}, err
	}
	if f.EventID, err = httpx.QueryUUID(r, "event_id"); err != nil {
		return ListFilter{}, err
	}
	if f.Since, err = httpx.QueryTime(r, "since"); err != nil {
		return ListFilter{}, err
	}
	if f.Until, err = httpx.QueryTime(r, "until"); err != nil {
		return ListFilter{}, err
	}
	if raw := r.URL.Query().Get("status"); raw != "" {
		status := Status(raw) // validated by the service
		f.Status = &status
	}
	return f, nil
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

// replay repeats one finished delivery. The replay is delivered asynchronously, hence 202.
func (c *Controller) replay(w http.ResponseWriter, r *http.Request) error {
	tenantID, err := httpx.RequireTenantID(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathUUID(r, "deliveryID", "delivery id")
	if err != nil {
		return err
	}
	d, err := c.svc.Replay(r.Context(), tenantID, id)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusAccepted, toResponse(d))
	return nil
}

// replayFailed repeats the failed deliveries of an endpoint within a time range.
func (c *Controller) replayFailed(w http.ResponseWriter, r *http.Request) error {
	tenantID, err := httpx.RequireTenantID(r)
	if err != nil {
		return err
	}
	endpointID, err := httpx.PathUUID(r, "endpointID", "endpoint id")
	if err != nil {
		return err
	}
	var req replayRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	var until time.Time
	if req.Until != nil {
		until = *req.Until
	}
	res, err := c.svc.ReplayFailed(r.Context(), tenantID, endpointID, req.Since, until)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusAccepted, replayResponse(res))
	return nil
}
