# simulated-vehicle

A **fleet simulator** that stands in for the physical vehicle devices. It demonstrates **device identity
bootstrapping** — how a factory-fresh vehicle earns its first credential and trades it for short-lived,
VIN-bound operational tokens. Port **8084** (status only).

## What it does (reconcile loop)
On an interval (default 8s) it:
1. **Discovers** vehicles from vehicle-service `GET /vehicles` (manufacturing persona) — the fleet starts
   empty and tracks every created vehicle.
2. For each new VIN, performs **factory burn-in**: obtains a factory workload token
   (`bootstrap.provision`) and calls identity-service `POST /bootstrap/provision` to register a freshly
   generated per-VIN bootstrap secret.
3. Exchanges VIN + bootstrap secret for a short-lived **`vehicle_bootstrap` JWT** (subject bound to the VIN).
4. **Registers** the device with vehicle-service `POST /vehicles/register`, then sends periodic
   **heartbeats** to keep it ONLINE.

Each call-home flow uses one fresh correlation id, so the audit log groups it as
`bootstrap_provisioned → service_token_issued → register_vehicle`.

## Notes
- It plays **both** the factory (burn-in) and the device (call-home) — a single-process simplification of two
  real-world actors coordinated by VIN.
- The bootstrap secret is a per-VIN **symmetric** secret (random 128-bit, `bootstrap-…`), held in memory and
  rotated whenever the simulator restarts. It is only ever traded for 5-min tokens, never used on business
  APIs. (A production system would use per-device asymmetric attestation keys.)
- Inter-service calls go through the shared `packages/clients/{identity,vehicle}` libraries.

## Endpoints
| Method/Path | Purpose |
|---|---|
| `GET /` | Fleet status: per-device `{vin, registered, vehicle_id, last_heartbeat, last_error}` |
| `GET /healthz` | Liveness |

## Key env vars
`PORT` (8084), `IDENTITY_URL`, `VEHICLE_URL`, `FACTORY_CLIENT_ID/SECRET`, `RECONCILE_INTERVAL`.

## Source map
- `main.go` — the entire fleet simulator: reconcile loop, burn-in, token exchange, register/heartbeat,
  status endpoint.
