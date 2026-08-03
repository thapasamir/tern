package endpoint

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/samirthapa/webhook-platform/internal/shared/httpx"
	"github.com/samirthapa/webhook-platform/internal/shared/pagination"
)

// Controller translates HTTP requests into service calls. It owns parsing and response
// shaping only; business rules live in Service.
type Controller struct {
	svc *Service
}

func NewController(svc *Service) *Controller { return &Controller{svc: svc} }

func (c *Controller) create(w http.ResponseWriter, r *http.Request) error {
	tenantID, err := httpx.RequireTenantID(r)
	if err != nil {
		return err
	}
	var req createEndpointRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	e, err := c.svc.Create(r.Context(), tenantID, CreateInput{
		URL:         req.URL,
		Description: req.Description,
		EventTypes:  req.EventTypes,
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, toResponseWithSecret(e))
	return nil
}

func (c *Controller) list(w http.ResponseWriter, r *http.Request) error {
	tenantID, err := httpx.RequireTenantID(r)
	if err != nil {
		return err
	}
	params, err := pagination.Parse(r)
	if err != nil {
		return err
	}
	res, err := c.svc.List(r.Context(), tenantID, params)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, pagination.ToPage(res, toResponse))
	return nil
}

func (c *Controller) get(w http.ResponseWriter, r *http.Request) error {
	tenantID, id, err := scope(r)
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

func (c *Controller) update(w http.ResponseWriter, r *http.Request) error {
	tenantID, id, err := scope(r)
	if err != nil {
		return err
	}
	var req updateEndpointRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	e, err := c.svc.Update(r.Context(), tenantID, id, UpdateInput(req))
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, toResponse(e))
	return nil
}

func (c *Controller) delete(w http.ResponseWriter, r *http.Request) error {
	tenantID, id, err := scope(r)
	if err != nil {
		return err
	}
	if err := c.svc.Delete(r.Context(), tenantID, id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (c *Controller) rotateSecret(w http.ResponseWriter, r *http.Request) error {
	tenantID, id, err := scope(r)
	if err != nil {
		return err
	}
	e, err := c.svc.RotateSecret(r.Context(), tenantID, id)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, toResponseWithSecret(e))
	return nil
}

func (c *Controller) disable(w http.ResponseWriter, r *http.Request) error {
	tenantID, id, err := scope(r)
	if err != nil {
		return err
	}
	e, err := c.svc.Disable(r.Context(), tenantID, id, DisabledReasonManual)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, toResponse(e))
	return nil
}

func (c *Controller) enable(w http.ResponseWriter, r *http.Request) error {
	tenantID, id, err := scope(r)
	if err != nil {
		return err
	}
	e, err := c.svc.Enable(r.Context(), tenantID, id)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, toResponse(e))
	return nil
}

// scope returns the authenticated tenant and the {endpointID} path parameter.
func scope(r *http.Request) (tenantID, endpointID uuid.UUID, err error) {
	if tenantID, err = httpx.RequireTenantID(r); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if endpointID, err = httpx.PathUUID(r, "endpointID", "endpoint id"); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return tenantID, endpointID, nil
}
