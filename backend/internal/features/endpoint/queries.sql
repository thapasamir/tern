-- Every query is scoped by tenant_id so one tenant can never read or change another's endpoints.

-- name: InsertEndpoint :exec
INSERT INTO endpoints (id, tenant_id, url, description, event_types, secret, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: GetEndpoint :one
SELECT * FROM endpoints
WHERE id = $1 AND tenant_id = $2;

-- Keyset pagination: newest first, continuing after the last ID of the previous page.
-- name: ListEndpoints :many
SELECT * FROM endpoints
WHERE tenant_id = sqlc.arg(tenant_id) AND id < sqlc.arg(after)
ORDER BY id DESC
LIMIT sqlc.arg(row_limit);

-- name: UpdateEndpoint :execrows
UPDATE endpoints
SET url         = sqlc.arg(url),
    description = sqlc.arg(description),
    event_types = sqlc.arg(event_types),
    updated_at  = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id);

-- name: UpdateEndpointSecret :execrows
UPDATE endpoints
SET secret = sqlc.arg(secret), updated_at = sqlc.arg(at)
WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id);

-- name: DisableEndpoint :execrows
UPDATE endpoints
SET status          = 'disabled',
    disabled_reason = sqlc.arg(reason)::text,
    disabled_at     = sqlc.arg(at)::timestamptz,
    updated_at      = sqlc.arg(at)
WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id);

-- name: EnableEndpoint :execrows
UPDATE endpoints
SET status               = 'active',
    disabled_reason      = NULL,
    disabled_at          = NULL,
    consecutive_failures = 0, -- a re-enabled endpoint starts with a clean slate
    updated_at           = sqlc.arg(at)
WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id);

-- name: DeleteEndpoint :execrows
DELETE FROM endpoints
WHERE id = $1 AND tenant_id = $2;

-- Endpoint health, driven by the delivery worker. Counting is not a configuration change, so
-- updated_at is left alone.

-- name: IncrementEndpointFailures :one
UPDATE endpoints
SET consecutive_failures = consecutive_failures + 1
WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id)
RETURNING consecutive_failures;

-- Matching no row is fine here: it just means the counter was already zero.
-- name: ResetEndpointFailures :exec
UPDATE endpoints
SET consecutive_failures = 0
WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id) AND consecutive_failures > 0;

-- Disables an endpoint only if it is still active, so that exactly one caller learns that it
-- was the one to disable it (rows affected is 1) when several attempts fail at the same time.
-- name: DisableActiveEndpoint :execrows
UPDATE endpoints
SET status          = 'disabled',
    disabled_reason = sqlc.arg(reason)::text,
    disabled_at     = sqlc.arg(at)::timestamptz,
    updated_at      = sqlc.arg(at)::timestamptz
WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id) AND status = 'active';
