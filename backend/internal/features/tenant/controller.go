package tenant

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/samirthapa/webhook-platform/internal/shared/httpx"
)

// Controller translates HTTP requests into service calls. It owns parsing and response
// shaping only; business rules live in Service.
type Controller struct {
	svc *Service
}

func NewController(svc *Service) *Controller { return &Controller{svc: svc} }

// createTenant is an admin operation: it creates a tenant plus its first API key.
func (c *Controller) createTenant(w http.ResponseWriter, r *http.Request) error {
	var req createTenantRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	t, key, err := c.svc.CreateTenant(r.Context(), req.Name)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, createTenantResponse{
		Tenant: toTenantResponse(t),
		APIKey: toCreatedKeyResponse(key),
	})
	return nil
}

// createAPIKeyForTenant is an admin operation, e.g. recovery after a tenant revoked all its keys.
func (c *Controller) createAPIKeyForTenant(w http.ResponseWriter, r *http.Request) error {
	tenantID, err := httpx.PathUUID(r, "tenantID", "tenant id")
	if err != nil {
		return err
	}
	return c.issueKey(w, r, tenantID)
}

func (c *Controller) me(w http.ResponseWriter, r *http.Request) error {
	tenantID, err := httpx.RequireTenantID(r)
	if err != nil {
		return err
	}
	t, err := c.svc.GetTenant(r.Context(), tenantID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, toTenantResponse(t))
	return nil
}

func (c *Controller) listAPIKeys(w http.ResponseWriter, r *http.Request) error {
	tenantID, err := httpx.RequireTenantID(r)
	if err != nil {
		return err
	}
	keys, err := c.svc.ListAPIKeys(r.Context(), tenantID)
	if err != nil {
		return err
	}
	data := make([]apiKeyResponse, 0, len(keys))
	for _, k := range keys {
		data = append(data, toAPIKeyResponse(k))
	}
	httpx.JSON(w, http.StatusOK, listAPIKeysResponse{Data: data})
	return nil
}

func (c *Controller) createAPIKey(w http.ResponseWriter, r *http.Request) error {
	tenantID, err := httpx.RequireTenantID(r)
	if err != nil {
		return err
	}
	return c.issueKey(w, r, tenantID)
}

func (c *Controller) revokeAPIKey(w http.ResponseWriter, r *http.Request) error {
	tenantID, err := httpx.RequireTenantID(r)
	if err != nil {
		return err
	}
	keyID, err := httpx.PathUUID(r, "keyID", "api key id")
	if err != nil {
		return err
	}
	if err := c.svc.RevokeAPIKey(r.Context(), tenantID, keyID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (c *Controller) issueKey(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) error {
	var req createAPIKeyRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	key, err := c.svc.CreateAPIKey(r.Context(), tenantID, req.Name)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, toCreatedKeyResponse(key))
	return nil
}

func toCreatedKeyResponse(k CreatedKey) createdAPIKeyResponse {
	return createdAPIKeyResponse{apiKeyResponse: toAPIKeyResponse(k.APIKey), Key: k.Plain}
}
