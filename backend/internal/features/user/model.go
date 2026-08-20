package user

import (
	"time"

	"github.com/google/uuid"
)

// User is a person who can sign in. In this version every user belongs to exactly one tenant,
// the one their signup created, and has full control of it.
type User struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	Email        string // lower-cased
	PasswordHash string
	CreatedAt    time.Time
}

// Tenant is the account a user works in, as the user feature sees it. The tenant feature owns
// API keys and everything else about it.
type Tenant struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
}

// Account is a user together with their tenant.
type Account struct {
	User   User
	Tenant Tenant
}

// Session is a signed-in user's login. The token that proves it is never stored, only its hash.
type Session struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Principal is who a valid session token belongs to.
type Principal struct {
	UserID    uuid.UUID
	TenantID  uuid.UUID
	SessionID uuid.UUID
}

// Login is the result of signing up or in: the account, and the session token, which is shown
// once and is what the client sends as "Authorization: Bearer <token>".
type Login struct {
	Account   Account
	Token     string
	ExpiresAt time.Time
}
