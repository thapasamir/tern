-- +goose Up
CREATE TABLE users (
    id            uuid PRIMARY KEY,
    tenant_id     uuid        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    email         text        NOT NULL,                -- stored lower-cased; it is the login name
    password_hash text        NOT NULL,                -- argon2id, PHC string format
    created_at    timestamptz NOT NULL
);

-- One account per email address, across all tenants.
CREATE UNIQUE INDEX users_email_idx ON users (email);
CREATE INDEX users_tenant_id_idx ON users (tenant_id);

CREATE TABLE sessions (
    id         uuid PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash bytea       NOT NULL UNIQUE,            -- sha256 of the token; the token itself is never stored
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);

-- +goose Down
DROP TABLE sessions;
DROP TABLE users;
