-- Deletes up to row_limit events created before the cutoff, oldest first. Their deliveries and
-- delivery attempts go with them (ON DELETE CASCADE), so nothing is left pointing at a deleted event.
-- Working in small batches keeps each transaction short, so purging never blocks the live system.
-- name: DeleteEventsBefore :execrows
DELETE FROM events
WHERE id IN (
    SELECT id FROM events
    WHERE created_at < sqlc.arg(cutoff)::timestamptz
    ORDER BY created_at
    LIMIT sqlc.arg(row_limit)
);
