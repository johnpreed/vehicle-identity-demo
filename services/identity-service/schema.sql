-- identity-service schema (database: identity). Idempotent; applied on startup.

CREATE TABLE IF NOT EXISTS users (
    id           UUID PRIMARY KEY,
    username     TEXT UNIQUE NOT NULL,
    display_name TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS passkey_credentials (
    id              TEXT PRIMARY KEY,          -- base64url credential id
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_json JSONB NOT NULL,            -- serialized webauthn.Credential
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS sessions (
    token      TEXT PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    step_up_at TIMESTAMPTZ                      -- last successful passkey step-up
);

CREATE TABLE IF NOT EXISTS service_identities (
    client_id         TEXT PRIMARY KEY,
    client_secret     TEXT NOT NULL,
    subject           TEXT NOT NULL,           -- JWT sub, e.g. service:vehicle-service
    allowed_scopes    TEXT NOT NULL,           -- space-delimited
    allowed_audiences TEXT NOT NULL            -- space-delimited
);

CREATE TABLE IF NOT EXISTS vehicle_bootstrap_credentials (
    vin              TEXT PRIMARY KEY,
    bootstrap_secret TEXT NOT NULL,            -- factory-provisioned device secret
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
