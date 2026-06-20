# audit-service

The platform's **tamper-evident-ish record of who did what**. It accepts audit events from other services
and lets a security auditor search them. Port **8083**, database **`audit`**.

## Responsibilities
- **Accept audit events** from services at `POST /audit`. Writes require a short-lived JWT with the
  **`audit.write`** scope (audience `audit-service`), verified against identity-service's JWKS.
- **Stamp the writer.** Each stored event records `writer_sub` from the verified JWT — i.e. which workload
  actually wrote the line (an integrity signal).
- **Search** at `GET /audit/search`, restricted to the staff **`security_auditor`** persona. Supports
  filtering by `resource_id` (full or partial, case-insensitive substring), `action`, `decision`,
  `actor_id`, `correlation_id`, and `limit`.

## What gets audited
Both **business actions** (create/claim/invite/commands, allow *and* deny) and the **service-to-service
plumbing** emitted by identity-service: `service_token_issued`, `bootstrap_provisioned`, and
`signing_key_generated`. Every event carries a `correlation_id`, so one logical flow (e.g. a vehicle calling
home) can be traced end to end.

## Endpoints
| Method/Path | Auth | Purpose |
|---|---|---|
| `POST /audit` | JWT scope `audit.write` | Append an audit event |
| `GET /audit/search` | `X-Staff-Persona: security_auditor` | Query the audit log |
| `GET /healthz` | — | Liveness |

## Audit event fields
`id`, `correlation_id`, `actor_type`, `actor_id`, `action`, `resource_type`, `resource_id`,
`decision` (ALLOW/DENY), `reason`, `metadata_json`, `created_at`.

## Data (Postgres `audit`)
A single append-only-style `audit_logs` table (indexed by resource, correlation id, and time).

## Key env vars
`PORT` (8083), `AUDIT_DB`, `JWT_ISSUER`, `IDENTITY_URL` (for JWKS verification), `WEB_ORIGINS`.

## Source map
- `main.go` — wiring, JWKS verifier, routes (`audit.write` middleware on writes, persona check on search).
- `store.go` — Postgres access + search filters. `schema.sql` — embedded, idempotent schema.
