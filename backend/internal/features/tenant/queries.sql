-- name: InsertTenant :exec
INSERT INTO tenants (id, name, created_at)
VALUES ($1, $2, $3);

-- name: GetTenant :one
SELECT id, name, created_at
FROM tenants
WHERE id = $1;

-- name: InsertAPIKey :exec
INSERT INTO api_keys (id, tenant_id, name, prefix, key_hash, created_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListAPIKeys :many
SELECT id, tenant_id, name, prefix, created_at, last_used_at, revoked_at
FROM api_keys
WHERE tenant_id = $1
ORDER BY created_at DESC;

-- Idempotent: revoking an already revoked key keeps its original revoked_at.
-- name: RevokeAPIKey :execrows
UPDATE api_keys
SET revoked_at = COALESCE(revoked_at, now())
WHERE id = $1 AND tenant_id = $2;

-- name: FindActiveAPIKeyByHash :one
SELECT id, tenant_id, name, prefix, created_at, last_used_at, revoked_at
FROM api_keys
WHERE key_hash = $1 AND revoked_at IS NULL;

-- name: TouchAPIKey :exec
UPDATE api_keys
SET last_used_at = now()
WHERE id = $1;
