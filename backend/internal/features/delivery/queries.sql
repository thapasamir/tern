-- Everything the worker needs to make one attempt, in a single round trip.
-- Not tenant-scoped: the worker acts on behalf of the platform, and the job id came from our own queue.
-- name: GetDeliveryWork :one
SELECT d.id            AS delivery_id,
       d.tenant_id     AS tenant_id,
       d.status        AS delivery_status,
       d.attempt_count AS attempt_count,
       d.next_attempt_at AS next_attempt_at,
       d.endpoint_id   AS endpoint_id,
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
SET status          = sqlc.arg(status),
    attempt_count   = attempt_count + 1,
    next_attempt_at = NULL,
    completed_at    = sqlc.arg(at)::timestamptz,
    updated_at      = sqlc.arg(at)::timestamptz
WHERE id = sqlc.arg(id) AND status = 'pending'
RETURNING attempt_count;

-- Records a failed attempt that will be retried: the delivery stays pending, with its next due time.
-- Like CompleteDelivery, matching no row means another worker already handled it.
-- name: ScheduleRetry :one
UPDATE deliveries
SET attempt_count   = attempt_count + 1,
    next_attempt_at = sqlc.arg(next_attempt_at)::timestamptz,
    updated_at      = sqlc.arg(at)::timestamptz
WHERE id = sqlc.arg(id) AND status = 'pending'
RETURNING attempt_count;

-- Fails every pending delivery of an endpoint that has just been disabled, leaving an attempt
-- row behind so each delivery's log says why it ended. Returns how many deliveries it failed.
-- name: FailPendingForEndpoint :execrows
WITH failed AS (
    UPDATE deliveries
    SET status          = 'failed',
        attempt_count   = attempt_count + 1,
        next_attempt_at = NULL,
        completed_at    = sqlc.arg(at)::timestamptz,
        updated_at      = sqlc.arg(at)::timestamptz
    WHERE endpoint_id = sqlc.arg(endpoint_id) AND status = 'pending'
    RETURNING id, attempt_count
)
INSERT INTO delivery_attempts (id, delivery_id, attempt_number, error, duration_ms, started_at)
SELECT gen_random_uuid(), failed.id, failed.attempt_count, sqlc.arg(reason)::text, 0, sqlc.arg(at)::timestamptz
FROM failed;

-- Pending deliveries that are overdue: their job should have run but evidently has not, for
-- instance because Redis lost its data. The reconciler queues them again.
-- The updated_at condition keeps one batch from being picked again and again: queueing a delivery
-- touches it, so the reconciler moves on to the next ones while the queue works through these.
-- name: ListOverdueDeliveries :many
SELECT id FROM deliveries
WHERE status = 'pending'
  AND COALESCE(next_attempt_at, created_at) < sqlc.arg(due_before)::timestamptz
  AND updated_at < sqlc.arg(due_before)::timestamptz
ORDER BY COALESCE(next_attempt_at, created_at)
LIMIT sqlc.arg(row_limit);

-- name: InsertAttempt :exec
INSERT INTO delivery_attempts (id, delivery_id, attempt_number, status_code, error, response_body, duration_ms, started_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- A delivery as the API shows it: with the type of its event, the URL of its endpoint, and a
-- summary of its latest attempt (all NULL until the first attempt has finished).
-- name: GetDelivery :one
SELECT sqlc.embed(d),
       e.type  AS event_type,
       ep.url  AS endpoint_url,
       la.status_code AS last_status_code,
       la.error       AS last_error,
       la.duration_ms AS last_duration_ms,
       la.started_at  AS last_started_at
FROM deliveries d
JOIN events e     ON e.id  = d.event_id
JOIN endpoints ep ON ep.id = d.endpoint_id
-- The latest attempt is the one numbered attempt_count (every attempt bumps it), found by the
-- unique (delivery_id, attempt_number) index.
LEFT JOIN delivery_attempts la ON la.delivery_id = d.id AND la.attempt_number = d.attempt_count
WHERE d.id = $1 AND d.tenant_id = $2;

-- name: ListAttempts :many
SELECT * FROM delivery_attempts
WHERE delivery_id = $1
ORDER BY attempt_number;

-- The delivery log. Every filter is optional (a NULL argument means "no filter"); created_at is the
-- delivery's creation time, in [since, until). Keyset pagination: newest first, continuing after
-- the last ID of the previous page.
-- name: ListDeliveries :many
SELECT sqlc.embed(d),
       e.type  AS event_type,
       ep.url  AS endpoint_url,
       la.status_code AS last_status_code,
       la.error       AS last_error,
       la.duration_ms AS last_duration_ms,
       la.started_at  AS last_started_at
FROM deliveries d
JOIN events e     ON e.id  = d.event_id
JOIN endpoints ep ON ep.id = d.endpoint_id
-- The latest attempt is the one numbered attempt_count (every attempt bumps it), found by the
-- unique (delivery_id, attempt_number) index.
LEFT JOIN delivery_attempts la ON la.delivery_id = d.id AND la.attempt_number = d.attempt_count
WHERE d.tenant_id = sqlc.arg(tenant_id)
  AND d.id < sqlc.arg(after)
  AND (sqlc.narg(endpoint_id)::uuid IS NULL OR d.endpoint_id = sqlc.narg(endpoint_id))
  AND (sqlc.narg(event_id)::uuid IS NULL OR d.event_id = sqlc.narg(event_id))
  AND (sqlc.narg(status)::text IS NULL OR d.status = sqlc.narg(status))
  AND (sqlc.narg(since)::timestamptz IS NULL OR d.created_at >= sqlc.narg(since))
  AND (sqlc.narg(until)::timestamptz IS NULL OR d.created_at < sqlc.narg(until))
ORDER BY d.id DESC
LIMIT sqlc.arg(row_limit);

-- Outbox sweeper: pending deliveries whose Redis enqueue never happened.
-- name: ListUnenqueuedDeliveries :many
SELECT id FROM deliveries
WHERE status = 'pending' AND enqueued_at IS NULL AND created_at < sqlc.arg(older_than)::timestamptz
ORDER BY created_at
LIMIT sqlc.arg(row_limit);

-- Records that the deliveries are in the queue. Safe to repeat: the first enqueue time is kept.
-- name: MarkDeliveriesEnqueued :exec
UPDATE deliveries
SET enqueued_at = COALESCE(enqueued_at, sqlc.arg(at)::timestamptz), updated_at = sqlc.arg(at)::timestamptz
WHERE id = ANY(sqlc.arg(ids)::uuid[]);

-- ---- Replay -------------------------------------------------------------------------------
-- A replay is a new delivery pointing back (replayed_from) at the one it repeats.

-- A delivery with what replaying it needs to know about its endpoint.
-- name: GetReplaySource :one
SELECT d.id, d.tenant_id, d.event_id, d.endpoint_id, d.status,
       (ep.status = 'active') AS endpoint_active
FROM deliveries d
JOIN endpoints ep ON ep.id = d.endpoint_id
WHERE d.id = $1 AND d.tenant_id = $2;

-- name: GetEndpointActive :one
SELECT (status = 'active') AS active
FROM endpoints
WHERE id = $1 AND tenant_id = $2;

-- Bulk insert through the Postgres COPY protocol.
-- name: InsertReplays :copyfrom
INSERT INTO deliveries (id, tenant_id, event_id, endpoint_id, status, replayed_from, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- The failed deliveries of an endpoint, in a time range, that have not been replayed yet, locked
-- for this transaction. Locking with SKIP LOCKED makes two simultaneous bulk replays split the
-- work instead of both replaying the same deliveries; NOT EXISTS makes a repeated call a no-op
-- for deliveries that already have a replay.
-- name: ClaimFailedForReplay :many
SELECT d.id, d.event_id
FROM deliveries d
WHERE d.tenant_id = sqlc.arg(tenant_id)
  AND d.endpoint_id = sqlc.arg(endpoint_id)
  AND d.status = 'failed'
  AND d.created_at >= sqlc.arg(since)::timestamptz
  AND d.created_at <  sqlc.arg(until)::timestamptz
  AND NOT EXISTS (SELECT 1 FROM deliveries r WHERE r.replayed_from = d.id)
ORDER BY d.id
LIMIT sqlc.arg(row_limit)
FOR UPDATE OF d SKIP LOCKED;
