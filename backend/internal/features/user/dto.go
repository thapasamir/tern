package user

import "time"

type signupRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	TenantName string `json:"tenant_name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type tenantResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// accountResponse is what /auth/me returns.
type accountResponse struct {
	User   userResponse   `json:"user"`
	Tenant tenantResponse `json:"tenant"`
}

type sessionResponse struct {
	// Token is shown once. Send it as "Authorization: Bearer <token>" to use the API as this user.
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// loginResponse is what signing up or in returns.
type loginResponse struct {
	accountResponse
	Session sessionResponse `json:"session"`
}

func toAccountResponse(a Account) accountResponse {
	return accountResponse{
		User:   userResponse{ID: a.User.ID.String(), Email: a.User.Email, CreatedAt: a.User.CreatedAt},
		Tenant: tenantResponse{ID: a.Tenant.ID.String(), Name: a.Tenant.Name, CreatedAt: a.Tenant.CreatedAt},
	}
}

func toLoginResponse(l Login) loginResponse {
	return loginResponse{
		accountResponse: toAccountResponse(l.Account),
		Session:         sessionResponse{Token: l.Token, ExpiresAt: l.ExpiresAt},
	}
}
