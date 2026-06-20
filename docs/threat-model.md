# Threat Model

A lightweight threat model for the demo. It is scoped to the identity platform mechanics; it is **not** a
production security review. For each threat we note the mitigation present in the demo and, where relevant,
what a production system would add.

| # | Threat | Mitigation in this demo | Production hardening |
|---|---|---|---|
| 1 | **Stolen session** | Consumer sessions are opaque, server-stored tokens in an **HttpOnly**, SameSite=Lax cookie (not readable by JS, reducing XSS theft). High-risk actions require a **fresh passkey step-up**, so a stolen cookie alone cannot start a vehicle. | Short session TTL + rotation, device binding, TLS-only `Secure` cookies, anomaly detection, revocation list. |
| 2 | **Replayed command** | `start_vehicle` requires an **idempotency key**; a replayed key returns the prior decision instead of re-executing. Step-up freshness is time-bounded (5 min), so a captured request cannot be replayed indefinitely. | Server-generated single-use nonces, signed request bodies, tight clock skew handling, per-command expiry. |
| 3 | **Compromised bootstrap secret** | Device secrets are **only** exchanged for short-lived (5-min) JWTs and are never used to call business APIs. The issued token's `sub` is **bound to the VIN**, and vehicle-service rejects registration when the token subject's VIN ≠ the registering VIN. Registration is also gated on the vehicle having been created. | Per-device asymmetric attestation keys (no shared secret), hardware-backed keystore, certificate-based device identity, secret rotation/revocation. |
| 4 | **Unauthorized staff action** | Staff actions are authorized **server-side** by vehicle-service using explicit per-persona checks (`CanCreateVehicle`, `CanAssignOwner`, …). The UI cannot bypass them; e.g. `manufacturing` cannot `assign_owner` and `sales_support` cannot `create_vehicle`. Every denial is audited. | Real staff authentication (SSO/MFA), signed persona/role claims instead of a header, separation of duties, approval workflows. |
| 5 | **Audit tampering** | Audit writes require a **JWT with the `audit.write` scope** (audience `audit-service`); unauthenticated writes are rejected (401). Each stored event records the **writer's verified `sub`**. Reads require the `security_auditor` persona. | Append-only / WORM storage, hash-chaining or signing of events, segregated audit datastore, tamper-evident export. |
| 6 | **Token audience confusion** | Every JWT carries an `aud` claim and **every verifier enforces it**. A token minted for `vehicle-service` is rejected by `audit-service` and vice-versa (verified in tests and by hand). Scopes (`audit.write`, `vehicle.register`, …) further narrow what a token may do. | Audience allow-lists per endpoint, distinct keys per audience if needed, explicit `azp`/resource indicators. |
| 7 | **Stale / reused JWT** | Tokens are **short-lived (5 min)** and verifiers enforce `exp` (expiration required). Each token has a unique **`jti`**. Signatures are Ed25519 and verified against JWKS, so tokens cannot be forged or silently extended. | `jti` denylist for one-time tokens, even shorter TTLs, proof-of-possession (DPoP/mTLS) to stop token replay across clients, key rotation. |

## Trust boundaries

- **Browser ↔ identity-service**: passkey ceremonies + session issuance. The private signing key and all
  credentials live here; the browser only holds an opaque session cookie.
- **Browser ↔ vehicle-service**: the cookie is *introspected* via identity-service; vehicle-service never
  trusts client-supplied identity claims.
- **Service ↔ service**: only short-lived, audience- and scope-scoped JWTs cross this boundary; verified
  against JWKS. No shared signing secret exists (no HMAC).
- **Device ↔ platform**: the factory secret is confined to the bootstrap exchange; everything afterward
  uses a VIN-bound short-lived token.
- **Staff ↔ platform**: *explicitly out of scope for authentication* in this demo (persona header).
  Authorization, however, is fully enforced server-side and audited.

## Known demo-only simplifications

These are intentional to keep the demo small; they are **not** production-safe:

- Staff personas are unauthenticated (a header). Production needs real staff auth.
- The Ed25519 signing key is generated in-memory on startup (rotates on restart) and is not persisted to a
  KMS/HSM.
- Vehicle bootstrap secrets are seeded shared secrets rather than per-device attestation keys.
- Services run over plain HTTP on localhost (no TLS); cookies are not `Secure`.
- Audit storage is a normal Postgres table (not append-only / tamper-evident).
