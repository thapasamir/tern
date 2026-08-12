-- Read-only reporting over deliveries and their attempts. Every query is scoped to one tenant, and
-- every filter is optional (a NULL argument means "no filter").

-- How many deliveries created in [since, until) are in each status.
-- name: CountDeliveriesByStatus :many
SELECT status, count(*)::bigint AS total
FROM deliveries
WHERE tenant_id = sqlc.arg(tenant_id)
  AND created_at >= sqlc.arg(since)::timestamptz
  AND created_at <  sqlc.arg(until)::timestamptz
  AND (sqlc.narg(endpoint_id)::uuid IS NULL OR endpoint_id = sqlc.narg(endpoint_id))
GROUP BY status;

-- How long the customers' servers took to answer. Only attempts that got an HTTP response count:
-- timeouts and refused connections have no response time, and the synthetic attempts recorded for
-- disabled endpoints made no request at all.
-- name: AttemptResponseTimes :one
SELECT count(*)::bigint AS attempts,
       coalesce(avg(a.duration_ms), 0)::float8 AS avg_ms,
       coalesce(percentile_cont(0.5)  WITHIN GROUP (ORDER BY a.duration_ms), 0)::float8 AS p50_ms,
       coalesce(percentile_cont(0.95) WITHIN GROUP (ORDER BY a.duration_ms), 0)::float8 AS p95_ms
FROM delivery_attempts a
JOIN deliveries d ON d.id = a.delivery_id
WHERE d.tenant_id = sqlc.arg(tenant_id)
  AND a.started_at >= sqlc.arg(since)::timestamptz
  AND a.started_at <  sqlc.arg(until)::timestamptz
  AND a.status_code IS NOT NULL
  AND (sqlc.narg(endpoint_id)::uuid IS NULL OR d.endpoint_id = sqlc.narg(endpoint_id));

-- Deliveries per time bucket and status, for charts. Buckets are cut in UTC whatever the session's
-- time zone, and only buckets that contain deliveries are returned.
-- name: DeliverySeries :many
SELECT (date_trunc(sqlc.arg(unit)::text, created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')::timestamptz AS bucket,
       status,
       count(*)::bigint AS total
FROM deliveries
WHERE tenant_id = sqlc.arg(tenant_id)
  AND created_at >= sqlc.arg(since)::timestamptz
  AND created_at <  sqlc.arg(until)::timestamptz
  AND (sqlc.narg(endpoint_id)::uuid IS NULL OR endpoint_id = sqlc.narg(endpoint_id))
GROUP BY bucket, status
ORDER BY bucket;
