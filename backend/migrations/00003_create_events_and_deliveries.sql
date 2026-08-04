-- +goose Up
CREATE TABLE events (
    id              uuid PRIMARY KEY,
    tenant_id       uuid        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    type            text        NOT NULL,
    payload         jsonb       NOT NULL,
    idempotency_key text,
    created_at      timestamptz NOT NULL
);

-- Serves "list a tenant's events, newest first" (UUIDv7 ids sort by creation time).
CREATE INDEX events_tenant_id_id_idx ON events (tenant_id, id DESC);

-- A client retrying a send with the same Idempotency-Key must not create a second event.
CREATE UNIQUE INDEX events_tenant_idempotency_key_idx
    ON events (tenant_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- One delivery = one event going to one endpoint. Its attempts are in delivery_attempts.
CREATE TABLE deliveries (
    id            uuid PRIMARY KEY,
    tenant_id     uuid        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    event_id      uuid        NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    endpoint_id   uuid        NOT NULL REFERENCES endpoints (id) ON DELETE CASCADE,
    status        text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'succeeded', 'failed')),
    attempt_count integer     NOT NULL DEFAULT 0,
    -- Set once the job is in the Redis queue. NULL on a pending row means "not queued yet":
    -- the outbox sweeper picks those up, so a crash between COMMIT and the Redis write loses nothing.
    enqueued_at   timestamptz,
    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL,
    completed_at  timestamptz,

    -- A delivery is pending exactly when it has not completed.
    CONSTRAINT deliveries_completed_consistency CHECK ((status = 'pending') = (completed_at IS NULL))
);

CREATE INDEX deliveries_event_id_id_idx ON deliveries (event_id, id DESC);
CREATE INDEX deliveries_endpoint_id_id_idx ON deliveries (endpoint_id, id DESC);

-- Tiny partial index for the outbox sweeper: only rows that still need queueing.
CREATE INDEX deliveries_unenqueued_idx ON deliveries (created_at)
    WHERE status = 'pending' AND enqueued_at IS NULL;

CREATE TABLE delivery_attempts (
    id             uuid PRIMARY KEY,
    delivery_id    uuid        NOT NULL REFERENCES deliveries (id) ON DELETE CASCADE,
    attempt_number integer     NOT NULL,
    status_code    integer,            -- NULL when no HTTP response was received
    error          text,               -- why the attempt failed, if it did
    response_body  text,               -- first bytes of the response, for debugging
    duration_ms    integer     NOT NULL,
    started_at     timestamptz NOT NULL,

    -- Two workers racing on the same delivery cannot both record "attempt 1".
    CONSTRAINT delivery_attempts_number_unique UNIQUE (delivery_id, attempt_number)
);

-- +goose Down
DROP TABLE delivery_attempts;
DROP TABLE deliveries;
DROP TABLE events;
