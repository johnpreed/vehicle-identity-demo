# Architecture

`vehicle-identity-demo` is a local-only, Docker Compose demonstration of an **identity platform for
vehicles**. It is deliberately small and readable; the focus is the identity machinery (authentication,
workload identity, authorization, step-up, audit), not a polished consumer product.

## Actors

| Actor | Who | How they authenticate |
|---|---|---|
| **Consumer** | Vehicle owners and drivers using `consumer-web` | **Real WebAuthn passkey** → session cookie issued by identity-service |
| **Staff** | Internal operators using `staff-web` | **Persona selector** (`X-Staff-Persona` header) — demo-only, no passkey |
| **Service** | Backend workloads (e.g. vehicle-service calling audit-service) | **Short-lived Ed25519 JWT** from identity-service (`service_credential` grant) |
| **Vehicle** | The `simulated-vehicle` device firmware | **Short-lived Ed25519 JWT** from identity-service (`vehicle_bootstrap` grant: VIN + factory secret) |

## Components

```
consumer-web (5173) ──passkey + session cookie──▶ identity-service (8081)
        │                                            │  • WebAuthn (signup/signin/step-up)
        │  session cookie                            │  • sessions
        ▼                                            │  • Ed25519 JWT issuer + JWKS
 vehicle-service (8082) ──introspect /me────────────▶│  • workload token grants
        │   • vehicle state (6 dimensions)
        │   • ownership / grants / invites
        │   • OWNS authorization (Can* funcs)
        │   • high-risk step-up enforcement
        ├──S2S JWT (audit.write)──▶ audit-service (8083) ──▶ Postgres (audit)
        ▼
 Postgres (vehicle)                                  Postgres (identity)
        ▲
        │  S2S JWT (vehicle.register / vehicle.heartbeat)
 simulated-vehicle (8084) ──bootstrap: VIN+secret──▶ identity-service ──▶ JWT

staff-web (5174) ──X-Staff-Persona──▶ vehicle-service / audit-service
```

Each service owns its own Postgres database (`identity`, `vehicle`, `audit`); services never share tables
and only communicate over HTTP.

## Why JWTs for service-to-service instead of HMAC

Service-to-service calls are authenticated with **short-lived (5-minute) Ed25519-signed JWTs**, verified
against identity-service's **JWKS** endpoint. We deliberately **do not use HMAC**:

- **Asymmetric keys = no shared secret to verify.** With HMAC, every verifier needs the *same* secret used
  to sign, so the signing secret must be distributed to all verifiers — any compromised verifier can now
  *forge* tokens. With Ed25519, only identity-service holds the private key; every other service verifies
  with the **public** key from JWKS and can never mint tokens.
- **Clear issuer/verifier separation.** identity-service is the single issuer; vehicle-service and
  audit-service are pure verifiers. This matches a real workload-identity / OIDC model.
- **Key rotation is a publish, not a redistribution.** Rotating keys means publishing a new JWK (with a new
  `kid`); verifiers pick it up automatically. No secret has to be redeployed to every service.
- **Standard claims do the heavy lifting.** `iss`, `aud`, `exp`, `scope`, and `jti` let each verifier
  enforce *who issued it*, *who it's for*, *when it dies*, *what it may do*, and *that it's a distinct token*.

Tokens carry the claims shown below; every verifier checks **signature, issuer, audience, expiry, and
scope** (see `packages/shared/jwt`).

```json
{
  "iss": "vehicle-demo.identity-service",
  "sub": "service:vehicle-service",
  "aud": "audit-service",
  "scope": "audit.write",
  "iat": 1710000000,
  "exp": 1710000300,
  "jti": "unique-token-id"
}
```

## Local trust model

- **identity-service is the root of trust.** It generates an Ed25519 keypair on startup and publishes the
  public key at `/.well-known/jwks.json`. All JWT verification chains back to this key.
- **Consumer sessions** are opaque cookies (`vid_session`, HttpOnly, SameSite=Lax). vehicle-service never
  re-implements session logic — it **introspects** the cookie by calling identity-service `/me`, so
  identity-service stays the single source of truth for *who the user is* and *whether step-up is fresh*.
- **Vehicle device credentials are factory-provisioned.** Each device has a VIN and a bootstrap secret. The
  seeded demo device (`VIN-DEMO-0001`) ships with a secret pre-seeded into identity-service. For any other
  vehicle a manufacturing operator creates, the **fleet simulator** performs factory "burn-in": it provisions
  a fresh bootstrap credential at identity-service (using a scoped `bootstrap.provision` factory workload
  token) before the device calls home. At runtime the device exchanges VIN + bootstrap secret for a
  short-lived JWT — the long-lived secret never leaves the device boundary and is never used to call business
  APIs.
- **Workload credentials** (e.g. vehicle-service's and the vehicle-factory's `client_id`/`client_secret`)
  are used only to obtain short-lived JWTs from identity-service, scoped to a specific audience and scope.
- **Staff personas are unauthenticated** by design — this is a demo of *authorization*, not staff SSO.
  Authorization for staff actions is still enforced server-side by vehicle-service / audit-service.

## Vehicle lifecycle & state model

Vehicle state is modeled as **independent dimensions**, not one flat enum, so operational state (locked,
powered, climate) is orthogonal to lifecycle and ownership:

| Dimension | Values |
|---|---|
| `lifecycle_status` | MANUFACTURED → PROVISIONED → REGISTERED → CLAIMABLE → CLAIMED → RETIRED |
| `ownership_state` | UNASSIGNED → CLAIM_PENDING → OWNER_ASSIGNED |
| `access_state` | LOCKED / UNLOCKED |
| `power_state` | OFF / ACCESSORY / READY / STARTED |
| `climate_state` | OFF / HEATING / COOLING / AUTO |
| `connectivity_state` | OFFLINE / ONLINE / DEGRADED |

Lifecycle transitions in the demo:

1. **Create** (manufacturing staff) → `MANUFACTURED`, a claim code is generated.
2. **Burn-in** (fleet simulator, for non-seeded VINs) → a bootstrap credential is provisioned at
   identity-service via a `bootstrap.provision` factory workload token.
3. **Register** (the device calls home with a VIN-bound `vehicle_bootstrap` JWT) → device identity recorded,
   `lifecycle = CLAIMABLE`, `connectivity = ONLINE`. Registration is *gated on the vehicle having been
   created* and on the token subject matching the VIN. The `simulated-vehicle` service runs as a **fleet
   simulator**: it discovers every created vehicle and brings its device online, so *any* vehicle a
   manufacturing operator creates calls home automatically (not just the seeded demo VIN).
4. **Claim** (consumer with VIN + claim code) or **Assign owner** (sales_support override) →
   `lifecycle = CLAIMED`, `ownership = OWNER_ASSIGNED`, an `owner` grant is created.

## Authorization model

Authorization is **owned entirely by vehicle-service** — there is no separate policy service. Checks are
plain, explicit Go functions in `services/vehicle-service/authz.go` that take a resolved `Subject` and
return an allow/deny `Decision` with a human-readable reason.

A `Subject` is either a **consumer** (with a resource-scoped `Role` on the target vehicle) or **staff**
(with a `Persona`). Resource-scoped roles mean a user can be `owner` of one vehicle and `driver` of another.

| Action | Function | Allowed for |
|---|---|---|
| view_status | `CanViewStatus` | owner, co-owner, driver, viewer |
| unlock_doors | `CanUnlock` | owner, co-owner, driver |
| start_climate | `CanStartClimate` | owner, co-owner, driver |
| **start_vehicle** | `CanStartVehicle` | owner, co-owner, driver **+ fresh passkey step-up** |
| invite_driver | `CanInviteDriver` | owner, co-owner |
| assign_owner | `CanAssignOwner` | sales_support |
| create_vehicle | `CanCreateVehicle` | manufacturing |
| read_audit_logs | (audit-service) | security_auditor |

## Audit model

Every security-relevant action emits an audit event to audit-service, **whether allowed or denied**.
audit-service requires a JWT with the `audit.write` scope (audience `audit-service`) for writes, and the
`security_auditor` persona for searches.

Audit fields: `id`, `correlation_id`, `actor_type`, `actor_id`, `action`, `resource_type`, `resource_id`,
`decision` (ALLOW/DENY), `reason`, `metadata_json`, `created_at`.

A **correlation id** is generated at the edge (`X-Correlation-Id`) and propagated through every
service-to-service hop and into the audit event, so a single user action can be traced end to end.

### Service-to-service & key-lifecycle events

The audit log captures not just business actions but the **service-to-service (S2S) plumbing**, so an
auditor can see every workload-to-workload interaction the identity platform brokers:

- **`service_token_issued`** — emitted by identity-service every time it mints a workload JWT (ALLOW), and
  on every rejected token request (DENY: bad bootstrap secret, invalid client credentials, disallowed
  audience/scope). The actor is the requesting workload (`sub`), the resource is the target service
  (`aud`), and metadata records `grant_type`, `scope`, `jti`, and `expires_at`. Because a token is minted
  whenever one service needs to call another, this is the canonical record of S2S calls.
- **`bootstrap_provisioned`** — emitted when the factory burns in a vehicle's bootstrap credential.
- **`signing_key_generated`** — emitted when identity-service generates its Ed25519 signing key (at
  startup). Restarting the service generates a new key and logs a fresh event — i.e. **key/"certificate"
  rotation shows up in the audit log** (metadata records the `kid` and `alg`).

Because these S2S events carry the same `correlation_id` as the business action that triggered them, one
call-home flow groups together as, e.g.: `bootstrap_provisioned` → `service_token_issued` →
`register_vehicle` under a single correlation id.

## High-risk command step-up

`start_vehicle` is the high-risk command and has three independent requirements:

1. **Role** — the caller must be `owner`, `co-owner`, or `driver` (a `viewer` is denied and the denial is
   audited).
2. **Fresh passkey step-up** — the caller must have completed a WebAuthn assertion within the last 5
   minutes. vehicle-service learns this from identity-service `/me` (`step_up_fresh`). If step-up is stale,
   vehicle-service returns `428 Precondition Required`; consumer-web then runs the step-up ceremony and
   retries.
3. **Idempotency key** — the request must carry an `idempotency_key`; a replayed key returns the prior
   decision instead of re-executing, defending against replayed commands.

The decision (and the step-up freshness at decision time) is always written to the audit log.
