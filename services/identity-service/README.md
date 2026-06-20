# identity-service

The platform's **root of trust**. It authenticates consumers with passkeys, issues short-lived
service-to-service tokens, and publishes the keys used to verify them. Port **8081**, database **`identity`**.

## Responsibilities
- **Consumer authentication** via real **WebAuthn** passkeys (sign-up, sign-in) and **step-up** for high-risk
  actions. Issues an HTTP-only `vid_session` cookie and is the single source of truth for sessions.
- **Workload token issuer.** Mints short-lived (5-min) **Ed25519** JWTs for service-to-service and device
  calls, and publishes the public key as a **JWKS**. (No HMAC — asymmetric signing only.)
- **Factory bootstrap provisioning.** Lets the vehicle factory register per-VIN device credentials.
- **Self-audits** its security-relevant operations (token issuance, provisioning, signing-key lifecycle).

## Endpoints
| Method/Path | Purpose |
|---|---|
| `POST /signup/start` · `/signup/finish` | WebAuthn registration ceremony |
| `POST /signin/start` · `/signin/finish` | WebAuthn login ceremony |
| `POST /step-up/start` · `/step-up/finish` | Passkey step-up (records freshness for high-risk commands) |
| `GET /me` | Session introspection: `{user, step_up_fresh, step_up_at}` |
| `POST /service-token` | Mint a workload JWT (`service_credential` or `vehicle_bootstrap` grant) |
| `POST /bootstrap/provision` | Factory burn-in: register a VIN's bootstrap secret (scope `bootstrap.provision`) |
| `GET /.well-known/jwks.json` | Public signing key (JWKS) for verifiers |
| `GET /healthz` | Liveness |

## JWTs
Ed25519, 5-minute lifetime, claims `iss, sub, aud, scope, iat, exp, jti`. Two grant types:
- `service_credential` — a backend workload proves `client_id`/`client_secret` (e.g. vehicle-service →
  `audit.write`, vehicle-factory → `bootstrap.provision`).
- `vehicle_bootstrap` — a device proves VIN + bootstrap secret; the issued token's `sub` is **bound to the
  VIN** (`service:simulated-vehicle:<VIN>`).

## Data (Postgres `identity`)
`users`, `passkey_credentials`, `sessions`, `service_identities`, `vehicle_bootstrap_credentials`.
On startup it self-seeds the `vehicle-service` and `vehicle-factory` workload clients (`selfSeed`).

## Key env vars
`PORT` (8081), `IDENTITY_DB`, `JWT_ISSUER`, `AUDIT_URL`, `WEBAUTHN_RP_ID` / `WEBAUTHN_RP_NAME` /
`WEBAUTHN_RP_ORIGINS`, `WEB_ORIGINS`, `VEHICLE_SERVICE_CLIENT_ID/SECRET`, `FACTORY_CLIENT_ID/SECRET`.

## Source map
- `main.go` — wiring, routes, self-seed, signing-key audit event.
- `app.go` — WebAuthn ceremonies, sessions, `/me`.
- `webauthn.go` — `waUser` adapter + in-flight ceremony store.
- `tokens.go` — `/service-token`, `/bootstrap/provision`, JWKS, token audit events.
- `store.go` — Postgres access. `schema.sql` — embedded, idempotent schema.
