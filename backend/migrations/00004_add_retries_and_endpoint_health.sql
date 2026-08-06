-- +goose Up

-- When a failed delivery is next due. NULL until the first retry is scheduled, and cleared again
-- once the delivery completes. Postgres is the source of truth for the schedule; the Redis queue
-- only mirrors it, so a lost queue can be rebuilt from this column.
ALTER TABLE deliveries ADD COLUMN next_attempt_at timestamptz;

-- Failed attempts in a row since the endpoint last succeeded (or was re-enabled).
-- Reaching the configured limit disables the endpoint.
ALTER TABLE endpoints ADD COLUMN consecutive_failures integer NOT NULL DEFAULT 0;

-- Serves the reconciler: "pending deliveries that are overdue". A delivery is due at its
-- next_attempt_at, or at creation time if it has not been retried yet.
CREATE INDEX deliveries_pending_due_idx ON deliveries ((COALESCE(next_attempt_at, created_at)))
    WHERE status = 'pending';

-- +goose Down
DROP INDEX deliveries_pending_due_idx;
ALTER TABLE endpoints DROP COLUMN consecutive_failures;
ALTER TABLE deliveries DROP COLUMN next_attempt_at;
