# vehicle-identity-demo

A **local-only** Docker Compose demo of an **identity platform for vehicles**. It shows a vehicle being
manufactured, calling home with a short-lived JWT *workload identity*, becoming claimable, being claimed by
a consumer who signs in with a **passkey (WebAuthn)**, shared with another driver via **resource-scoped
roles**, and executing **protected commands** with explicit authorization checks, **passkey step-up** for
high-risk actions, and full **audit logging**.

The focus is the identity platform — not a flashy UI.

## Stack
- **Go** backend services (`identity-service`, `vehicle-service`, `audit-service`, `simulated-vehicle`)
- **Postgres** for all durable storage (one DB per service)
- **React/Vite** web UIs (`consumer-web`, `staff-web`)
- **Ed25519** short-lived (5-minute) JWTs for service-to-service auth, published via **JWKS** (no HMAC)
- **Real WebAuthn** passkeys (RP ID `localhost`)
- Authorization is owned by `vehicle-service` (no separate policy service)

## Quick start
```bash
cp .env.example .env
make up        # docker compose up --build
make seed      # create a demo vehicle (the simulator burns it in and registers it)
make logs      # tail logs
make down      # stop + remove
make test      # run Go tests
```
Then open:
- Consumer web: http://localhost:5173
- Staff web:    http://localhost:5174

See [`docs/demo-script.md`](docs/demo-script.md) for the full guided demo, and
[`docs/architecture.md`](docs/architecture.md) / [`docs/threat-model.md`](docs/threat-model.md) for design.

## Ports
| Component | Port |
|---|---|
| consumer-web | 5173 |
| staff-web | 5174 |
| identity-service | 8081 |
| vehicle-service | 8082 |
| audit-service | 8083 |
| simulated-vehicle (status) | 8084 |
| postgres | 5432 |
