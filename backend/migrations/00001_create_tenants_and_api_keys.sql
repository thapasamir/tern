-- +goose Up
CREATE TABLE tenants (
    id         uuid PRIMARY KEY,
    name       text        NOT NULL,
    created_at timestamptz NOT NULL
);

CREATE TABLE api_keys (
    id           uuid PRIMARY KEY,
    tenant_id    uuid        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    prefix       text        NOT NULL,          -- first chars of the key, safe to display
    key_hash     bytea       NOT NULL UNIQUE,   -- sha256 of the full key; plaintext is never stored
    created_at   timestamptz NOT NULL,
    last_used_at timestamptz,
    revoked_at   timestamptz
);

CREATE INDEX api_keys_tenant_id_idx ON api_keys (tenant_id);

-- +goose Down
DROP TABLE api_keys;
DROP TABLE tenants;
