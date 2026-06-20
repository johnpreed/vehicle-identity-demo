# vehicle-service

Owns vehicle **lifecycle, state, ownership, and authorization**. This is where every
authorization decision is made — there is **no separate policy service**. Port **8082**, database **`vehicle`**.

## Responsibilities
- **Vehicle lifecycle & multi-dimensional state** (lifecycle / access / power / climate / connectivity /
  ownership — modeled as independent dimensions, not one flat enum).
- **Device registration** from the simulated vehicle (VIN-bound `vehicle_bootstrap` JWT).
- **Ownership**: consumer claiming (VIN + claim code), staff owner assignment, driver invitations & grants.
- **Protected commands**: unlock, start-climate, and the high-risk start-vehicle.
- **Authorization**: explicit `Can*` functions evaluate a resolved `Subject` (consumer role or staff persona).
- **Audit**: every security-relevant decision (allow *or* deny) is written to audit-service.

## Authorization model
Plain, explicit Go functions in `authz.go` — `CanViewStatus`, `CanUnlock`, `CanStartClimate`,
`CanStartVehicle`, `CanInviteDriver`, `CanAssignOwner`, `CanCreateVehicle`. Consumer roles: owner, co-owner,
driver, viewer. Staff personas: manufacturing, sales_support, security_auditor.

`start_vehicle` is the **high-risk** command: requires owner/co-owner/driver **AND** a fresh passkey step-up
(≤5 min, learned from identity-service `/me`) **AND** an idempotency key; it always audits the outcome.

## Endpoints
| Method/Path | Purpose |
|---|---|
| `POST /staff/vehicles/create` | Staff (manufacturing) creates a vehicle |
| `POST /staff/vehicles/{id}/assign-owner` | Staff (sales_support) assigns an owner |
| `GET /vehicles` | List (staff: whole fleet; consumer: vehicles they have a grant on) |
| `GET /vehicles/lookup?vin=` | Resolve a CLAIMABLE vehicle by VIN (for claiming) |
| `GET /vehicles/{id}` | Vehicle detail + grants (authorized viewers) |
| `POST /vehicles/{id}/claim` | Consumer claims with a claim code → becomes owner |
| `POST /vehicles/{id}/invite` · `GET /invitations` · `POST /invitations/{code}/accept` | Driver invites |
| `POST /vehicles/{id}/commands/{unlock,start-climate,start-vehicle}` | Protected commands |
| `POST /vehicles/register` · `POST /vehicles/{id}/heartbeat` | Device calls (JWT-scoped) |
| `GET /healthz` | Liveness |

## How it authenticates callers
- **Consumers**: forwards the `vid_session` cookie to identity-service `/me` (via the identity client) to
  resolve the user + step-up freshness — it does **not** manage sessions itself.
- **Staff**: a demo-only `X-Staff-Persona` header (authorization is still enforced server-side).
- **Devices**: a `vehicle_bootstrap` JWT verified against identity-service's JWKS; `register` checks the
  token's VIN-bound subject matches the VIN.

## Data (Postgres `vehicle`)
`vehicles`, `vehicle_device_identities`, `vehicle_grants`, `vehicle_invitations`, `vehicle_commands`.

## Key env vars
`PORT` (8082), `VEHICLE_DB`, `JWT_ISSUER`, `IDENTITY_URL`, `AUDIT_URL`, `WEB_ORIGINS`,
`VEHICLE_SERVICE_CLIENT_ID/SECRET` (its workload identity for audit writes).

## Source map
- `main.go` — wiring (identity + audit clients), routes, JWT verifier.
- `authz.go` — the `Can*` authorization functions and `Subject`/`Decision` types.
- `handlers.go` — staff/consumer endpoints, subject resolution, audit emit.
- `commands.go` — protected commands incl. high-risk start-vehicle + device register/heartbeat.
- `store.go` — Postgres access. `schema.sql` — embedded, idempotent schema.
