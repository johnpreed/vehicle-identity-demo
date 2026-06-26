# Scaling & Load-Balancing Shortcomings

This demo runs **a single instance of each service** under Docker Compose. That assumption is baked into a
few places: some state lives **in process memory** rather than in Postgres. Behind a load balancer — e.g. a
Kubernetes **Deployment scaled to N replicas (Pods)** sitting behind a **Service** — that in-memory state is
**per-replica**, and requests for one logical flow can land on different replicas. This document enumerates
where that breaks, why, and what a production version would do instead.

> This complements the *Known demo-only simplifications* in [`threat-model.md`](./threat-model.md). Nothing
> here is a bug in the demo — these are deliberate single-instance simplifications that would need to change
> before horizontal scaling.

## The root cause

> **Per-replica in-memory state does not work behind a load balancer.**

A load balancer distributes requests across replicas with no guarantee that two related requests (e.g. the
*start* and *finish* of a ceremony, or *signing* a token and *serving* its public key) reach the same one.
Any state that must be consistent across those requests has to live in **shared storage** (Postgres/Redis)
or a **shared key service** (KMS/HSM), not in a Go map or a process-local variable.

## What breaks when scaled

| State | Where it lives today | Failure when scaled | Severity |
|---|---|---|---|
| **Ed25519 signing key** | In memory, generated at startup (`packages/shared/jwt/jwt.go` `NewIssuer` → `ed25519.GenerateKey`; wired in `services/identity-service/main.go`) | Each replica generates a **different keypair** with a different `kid`. A token signed by replica A is rejected when its `kid` is absent from the JWKS a verifier fetches from replica B. | **Outage** — intermittent token-verification failures |
| **WebAuthn ceremony store** | In-memory TTL'd map (`services/identity-service/webauthn.go` `ceremonyStore`, a `map[string]ceremony` behind a mutex) | `signup/signin/step-up` **start** stores the ceremony on replica A; **finish** lands on replica B, which has no record → `unknown or expired ceremony`. Passkey flows fail randomly. | **Broken auth** — sign-up / sign-in / step-up intermittently fail |
| **`jti` replay cache** (future) | Does not exist yet | If asymmetric client auth (`private_key_jwt`) is added, the assertion-replay `jti` cache would have the **same** problem unless shared: a replay could hit a different replica that hasn't seen the `jti`. | **Security gap** — only relevant if/when that feature lands |

## What is already safe (Postgres-backed)

Most durable state is already shared correctly, so it scales without change:

- **Consumer sessions & step-up freshness** — the `sessions` table, including `step_up_at`
  (`services/identity-service/store.go`). Any replica can validate a session cookie or check step-up
  freshness.
- **Command idempotency** — the `vehicle_commands` table; `FindCommand` / `InsertCommand` in
  `services/vehicle-service/store.go` make `start_vehicle` idempotent across replicas.
- **All business & audit data** — vehicles, ownership, grants, invites, and audit events live in their
  per-service Postgres databases.

This is the key point: the data layer is fine. The gaps are specifically the **in-memory** signing key and
the **in-memory** ceremony store.

## What to do for production

### 1. Stop generating the signing key in-process

This is the most important fix. Options, best to most pragmatic:

- **KMS/HSM-backed signing (preferred).** The private key lives in a managed KMS/HSM (AWS KMS, GCP KMS,
  Azure Key Vault, Vault Transit); identity-service calls a *sign* API and the key never leaves the KMS.
  All replicas share one `kid`; rotation is a KMS feature. Sign latency is hidden because a workload token
  is cached for its 5-minute life rather than re-signed per request.
- **Shared private key from a secret store.** Generate the keypair out-of-band and load the *same* key into
  every replica from a KMS-backed Kubernetes Secret, HashiCorp Vault, or the External Secrets Operator.
  Load it — don't generate it in `NewIssuer`.

In **both** cases, the JWKS endpoint must:

- **Advertise every currently-valid public key by `kid`.** Verifiers select the key matching the token's
  `kid` header.
- **Rotate with overlap.** Publish the new `kid` to JWKS *before* signing with it; keep the old public key
  in JWKS for at least **max token lifetime + verifier JWKS cache TTL** before retiring it. This gives
  zero-downtime rotation — the opposite of today's hard cutover on restart.
- Verifiers should **cache JWKS** (respecting `Cache-Control`) and **refetch on an unknown `kid`** so a
  freshly rotated key is picked up promptly.

### 2. Externalize the ceremony store

Move the in-memory `ceremonyStore` behind a shared backend (Redis or a Postgres table) keyed by
`ceremony_id`, with the same single-use + TTL semantics it has today. The existing `put` / `take` / `gc`
surface maps cleanly onto a swappable `CeremonyStore` interface, so the handler code need not change.

### 3. Share any future `jti` replay cache

If/when asymmetric client assertions are added (see the `private_key_jwt` discussion), back the replay
cache with the same shared store (short-TTL `jti` set in Redis/Postgres), for the same reason.

### Band-aid: sticky sessions

A load balancer with **session affinity** (sticky sessions) can keep a client pinned to one replica, which
*masks* the ceremony-store problem (start/finish hit the same Pod). It does **not** fix the signing-key
problem (any verifier may fetch JWKS from any replica), and it fails on replica restart/rescale. Treat it
as a stopgap, not a solution.

## Summary

| Concern | Demo (single instance) | Production (N replicas) |
|---|---|---|
| Signing key | In-memory per process | KMS/HSM or shared secret; JWKS multi-`kid` with overlapping rotation |
| Ceremony store | In-memory map | Shared Redis/Postgres store |
| `jti` replay cache (future) | N/A | Shared Redis/Postgres store |
| Sessions, step-up, idempotency, business data | Postgres (already shared) | Postgres (unchanged) |

Or: adopt a managed IdP and let it own signing-key management, JWKS rotation, and ceremony/replay state
entirely.
