-- +goose Up

-- A replay is a new delivery of the same event to the same endpoint. It points back at the
-- delivery it replays, so the original keeps its own log untouched and the two stay linked.
ALTER TABLE deliveries ADD COLUMN replayed_from uuid REFERENCES deliveries (id) ON DELETE SET NULL;

-- Serves "has this delivery already been replayed?", which bulk replay asks for every candidate.
CREATE INDEX deliveries_replayed_from_idx ON deliveries (replayed_from) WHERE replayed_from IS NOT NULL;

-- +goose Down
DROP INDEX deliveries_replayed_from_idx;
ALTER TABLE deliveries DROP COLUMN replayed_from;
