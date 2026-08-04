-- Everything the worker needs to make one attempt, in a single round trip.
-- Not tenant-scoped: the worker acts on behalf of the platform, and the job id came from our own queue.
-- name: GetDeliveryWork :one
SELECT d.id            AS delivery_id,
       d.tenant_id     AS tenant_id,
       d.status        AS delivery_status,
       d.event_id      AS event_id,
       e.type          AS event_type,
       e.payload       AS event_payload,
       e.created_at    AS event_created_at,
       ep.url          AS endpoint_url,
       ep.secret       AS endpoint_secret,
       (ep.status = 'active') AS endpoint_active
FROM deliveries d
JOIN events e     ON e.id  = d.event_id
JOIN endpoints ep ON ep.id = d.endpoint_id
WHERE d.id = $1;

-- Marks a pending delivery as finished. Matching no row means another worker already finished it,
-- which is how duplicate jobs are detected without any locking.
-- name: CompleteDelivery :one
UPDATE deliveries
SET status        = sqlc.arg(status),
    attempt_count = attempt_count + 1,
    completed_at  = sqlc.arg(at)::timestamptz,
    updated_at    = sqlc.arg(at)::timestamptz
WHERE id = sqlc.arg(id) AND status = 'pending'
RETURNING attempt_count;

-- name: InsertAttempt :exec
INSERT INTO delivery_attempts (id, delivery_id, attempt_number, status_code, error, response_body, duration_ms, started_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetDelivery :one
SELECT * FROM deliveries
WHERE id = $1 AND tenant_id = $2;

-- name: ListAttempts :many
SELECT * FROM delivery_attempts
WHERE delivery_id = $1
ORDER BY attempt_number;

-- Keyset pagination: newest first, continuing after the last ID of the previous page.
-- name: ListDeliveriesByEvent :many
SELECT * FROM deliveries
WHERE tenant_id = sqlc.arg(tenant_id)
  AND event_id = sqlc.arg(event_id)
  AND id < sqlc.arg(after)
ORDER BY id DESC
LIMIT sqlc.arg(row_limit);

-- Outbox sweeper: pending deliveries whose Redis enqueue never happened.
-- name: ListUnenqueuedDeliveries :many
SELECT id FROM deliveries
WHERE status = 'pending' AND enqueued_at IS NULL AND created_at < sqlc.arg(older_than)::timestamptz
ORDER BY created_at
LIMIT sqlc.arg(row_limit);

-- name: MarkDeliveriesEnqueued :exec
UPDATE deliveries
SET enqueued_at = sqlc.arg(at)::timestamptz, updated_at = sqlc.arg(at)::timestamptz
WHERE id = ANY(sqlc.arg(ids)::uuid[]) AND enqueued_at IS NULL;
