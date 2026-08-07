package tenant

import (
	"time"

	"github.com/samirthapa/tern/internal/shared/httpx"
)

type createTenantRequest struct {
	Name string `json:"name"`
}

type createAPIKeyRequest struct {
	Name string `json:"name"`
}

type tenantResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type apiKeyResponse struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

// createdAPIKeyResponse is the only response that ever contains the plaintext key.
type createdAPIKeyResponse struct {
	apiKeyResponse
	Key string `json:"key"`
}

type createTenantResponse struct {
	Tenant tenantResponse        `json:"tenant"`
	APIKey createdAPIKeyResponse `json:"api_key"`
}

type listAPIKeysResponse struct {
	Data []apiKeyResponse `json:"data"`
}

func toTenantResponse(t Tenant) tenantResponse {
	return tenantResponse{ID: t.ID.String(), Name: t.Name, CreatedAt: t.CreatedAt.UTC()}
}

func toAPIKeyResponse(k APIKey) apiKeyResponse {
	return apiKeyResponse{
		ID:         k.ID.String(),
		Name:       k.Name,
		Prefix:     k.Prefix,
		CreatedAt:  k.CreatedAt.UTC(),
		LastUsedAt: httpx.UTCPtr(k.LastUsedAt),
		RevokedAt:  httpx.UTCPtr(k.RevokedAt),
	}
}
