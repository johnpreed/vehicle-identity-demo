-- audit-service schema (database: audit). Idempotent; applied on startup.

CREATE TABLE IF NOT EXISTS audit_logs (
    id             UUID PRIMARY KEY,
    correlation_id TEXT NOT NULL DEFAULT '',
    actor_type     TEXT NOT NULL,
    actor_id       TEXT NOT NULL,
    action         TEXT NOT NULL,
    resource_type  TEXT NOT NULL,
    resource_id    TEXT NOT NULL,
    decision       TEXT NOT NULL,
    reason         TEXT NOT NULL DEFAULT '',
    metadata_json  JSONB NOT NULL DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS audit_logs_resource_idx ON audit_logs (resource_type, resource_id);
CREATE INDEX IF NOT EXISTS audit_logs_correlation_idx ON audit_logs (correlation_id);
CREATE INDEX IF NOT EXISTS audit_logs_created_idx ON audit_logs (created_at DESC);
