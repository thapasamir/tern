-- +goose Up

-- Serves the delivery log: "this tenant's deliveries, newest first", before any filter narrows it.
CREATE INDEX deliveries_tenant_id_id_idx ON deliveries (tenant_id, id DESC);

-- Serves retention: "events older than the cutoff", oldest first.
CREATE INDEX events_created_at_idx ON events (created_at);

-- +goose Down
DROP INDEX events_created_at_idx;
DROP INDEX deliveries_tenant_id_id_idx;
