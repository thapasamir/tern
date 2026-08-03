-- +goose Up
CREATE TABLE endpoints (
    id              uuid PRIMARY KEY,
    tenant_id       uuid        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    url             text        NOT NULL,
    description     text        NOT NULL DEFAULT '',
    event_types     text[]      NOT NULL DEFAULT '{}',   -- empty means "all event types"
    secret          text        NOT NULL,                -- HMAC key; must be recoverable, so not hashed
    status          text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    disabled_reason text,
    disabled_at     timestamptz,
    created_at      timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL,

    -- An endpoint is disabled exactly when it has a disabled_at timestamp.
    CONSTRAINT endpoints_disabled_consistency CHECK ((status = 'disabled') = (disabled_at IS NOT NULL))
);

-- Serves "list a tenant's endpoints, newest first" (UUIDv7 ids sort by creation time).
CREATE INDEX endpoints_tenant_id_id_idx ON endpoints (tenant_id, id DESC);

-- +goose Down
DROP TABLE endpoints;
