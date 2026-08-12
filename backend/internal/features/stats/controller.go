package stats

import (
	"net/http"

	"github.com/samirthapa/tern/internal/shared/httpx"
)

// Controller translates HTTP requests into service calls. It owns parsing and response
// shaping only; business rules live in Service.
type Controller struct {
	svc *Service
}

func NewController(svc *Service) *Controller { return &Controller{svc: svc} }

// deliveries reports on the tenant's deliveries: GET /stats/deliveries?since=&until=&endpoint_id=&interval=
func (c *Controller) deliveries(w http.ResponseWriter, r *http.Request) error {
	tenantID, err := httpx.RequireTenantID(r)
	if err != nil {
		return err
	}

	var in Input
	if in.EndpointID, err = httpx.QueryUUID(r, "endpoint_id"); err != nil {
		return err
	}
	if in.Since, err = httpx.QueryTime(r, "since"); err != nil {
		return err
	}
	if in.Until, err = httpx.QueryTime(r, "until"); err != nil {
		return err
	}
	in.Interval = r.URL.Query().Get("interval")

	report, err := c.svc.Report(r.Context(), tenantID, in)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, toResponse(report))
	return nil
}
