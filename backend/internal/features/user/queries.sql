-- Signing up creates the tenant, its first user and their session together, so this feature
-- inserts the tenant row itself instead of going through the tenant feature: the three inserts
-- must be one transaction.
-- name: InsertTenant :exec
INSERT INTO tenants (id, name, created_at)
VALUES ($1, $2, $3);

-- name: InsertUser :exec
INSERT INTO users (id, tenant_id, email, password_hash, created_at)
VALUES ($1, $2, $3, $4, $5);

-- name: InsertSession :exec
INSERT INTO sessions (id, user_id, token_hash, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5);

-- name: GetUserByEmail :one
SELECT id, tenant_id, email, password_hash, created_at
FROM users
WHERE email = $1;

-- name: GetAccount :one
SELECT u.id          AS user_id,
       u.email       AS email,
       u.created_at  AS user_created_at,
       t.id          AS tenant_id,
       t.name        AS tenant_name,
       t.created_at  AS tenant_created_at
FROM users u
JOIN tenants t ON t.id = u.tenant_id
WHERE u.id = $1;

-- name: FindSession :one
SELECT s.id AS session_id, s.user_id, u.tenant_id
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = sqlc.arg(token_hash)
  AND s.expires_at > sqlc.arg(now);

-- name: DeleteSession :execrows
DELETE FROM sessions WHERE id = $1;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions
WHERE user_id = sqlc.arg(user_id) AND expires_at <= sqlc.arg(now);
