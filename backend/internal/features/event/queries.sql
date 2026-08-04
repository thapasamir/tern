-- Ownership: this feature creates events and, as part of ingesting one, the delivery rows that
-- fan it out. The delivery feature owns everything that happens to a delivery afterwards.

-- Returns 0 rows affected when the idempotency key was already used by an earlier request.
-- name: InsertEvent :execrows
INSERT INTO events (id, tenant_id, type, payload, idempotency_key, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (tenant_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING;

-- name: GetEvent :one
SELECT * FROM events
WHERE id = $1 AND tenant_id = $2;

-- name: GetEventByIdempotencyKey :one
SELECT * FROM events
WHERE tenant_id = $1 AND idempotency_key = $2;

-- Fan-out targets: the tenant's active endpoints that subscribe to this event type
-- (an empty event_types list subscribes to everything).
-- FOR SHARE keeps a concurrent DELETE or UPDATE of these rows waiting until our transaction
-- commits, so we can never insert a delivery for an endpoint that vanished mid-flight.
-- name: ListMatchingEndpointIDs :many
SELECT id FROM endpoints
WHERE tenant_id = sqlc.arg(tenant_id)
  AND status = 'active'
  AND (cardinality(event_types) = 0 OR sqlc.arg(event_type)::text = ANY(event_types))
ORDER BY id
FOR SHARE;

-- Bulk insert through the Postgres COPY protocol: one round trip for the whole fan-out.
-- name: InsertDeliveries :copyfrom
INSERT INTO deliveries (id, tenant_id, event_id, endpoint_id, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);
