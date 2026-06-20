-- vehicle-service schema (database: vehicle). Idempotent; applied on startup.
-- Vehicle state is modeled as independent dimensions, not one flat enum.

CREATE TABLE IF NOT EXISTS vehicles (
    id                 UUID PRIMARY KEY,
    vin                TEXT UNIQUE NOT NULL,
    model              TEXT NOT NULL DEFAULT 'Demo EV',
    claim_code         TEXT NOT NULL,
    lifecycle_status   TEXT NOT NULL DEFAULT 'MANUFACTURED',
    access_state       TEXT NOT NULL DEFAULT 'LOCKED',
    power_state        TEXT NOT NULL DEFAULT 'OFF',
    climate_state      TEXT NOT NULL DEFAULT 'OFF',
    connectivity_state TEXT NOT NULL DEFAULT 'OFFLINE',
    ownership_state    TEXT NOT NULL DEFAULT 'UNASSIGNED',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_heartbeat_at  TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS vehicle_device_identities (
    vehicle_id    UUID PRIMARY KEY REFERENCES vehicles(id) ON DELETE CASCADE,
    subject       TEXT NOT NULL,          -- JWT sub presented by the device
    registered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS vehicle_grants (
    id         UUID PRIMARY KEY,
    vehicle_id UUID NOT NULL REFERENCES vehicles(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL DEFAULT '',  -- consumer uuid once known (empty if staff-assigned)
    username   TEXT NOT NULL,             -- stable principal key (unique in identity)
    role       TEXT NOT NULL,             -- owner / co-owner / driver / viewer
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (vehicle_id, username)
);

CREATE TABLE IF NOT EXISTS vehicle_invitations (
    code             TEXT PRIMARY KEY,
    vehicle_id       UUID NOT NULL REFERENCES vehicles(id) ON DELETE CASCADE,
    invited_username TEXT NOT NULL,
    role             TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'pending',  -- pending / accepted / revoked
    invited_by       TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    accepted_at      TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS vehicle_commands (
    id              UUID PRIMARY KEY,
    vehicle_id      UUID NOT NULL REFERENCES vehicles(id) ON DELETE CASCADE,
    command         TEXT NOT NULL,
    idempotency_key TEXT,
    actor_id        TEXT NOT NULL,
    decision        TEXT NOT NULL,
    reason          TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (vehicle_id, command, idempotency_key)
);
