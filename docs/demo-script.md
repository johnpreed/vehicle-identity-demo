# Demo Script

A guided, end-to-end walkthrough of the vehicle identity platform. Total time: ~10 minutes.

> **Passkeys are real WebAuthn.** Your browser/OS will prompt for a passkey on every consumer sign-up,
> sign-in, and step-up. Use a browser with a platform authenticator (Touch ID / Windows Hello) or a
> security key. You will create a **separate passkey per username** (e.g. `alice`, `bob`).

## 0. Start the stack

```bash
cp .env.example .env      # first time only
make up                   # build + start everything
make logs                 # (optional) watch logs in another terminal
```

Open the two UIs:

- **Consumer web:** http://localhost:5173
- **Staff web:** http://localhost:5174

The `simulated-vehicle` service runs as a **fleet simulator**: it watches for vehicles that manufacturing
creates and brings each one's device online automatically. You can watch the fleet at http://localhost:8084/.

---

## Flow 1 — Manufacturing (workload identity)

1. In **staff-web**, keep the **Manufacturing Operator** persona selected.
2. Under **Create vehicle**, either type a VIN such as **`VIN-DEMO-0001`** or **leave it blank** to
   auto-generate one, then click **Create**.
   - Result: vehicle is created as `MANUFACTURED` and a **claim code** is shown — note it.
3. Within ~5–10 seconds the **fleet simulator** discovers the new vehicle, provisions its bootstrap
   credential ("factory burn-in"), exchanges VIN + secret for a short-lived JWT, and registers it. Click
   **↻** on the **Fleet** card:
   - `lifecycle = CLAIMABLE`, `connectivity = ONLINE`.
4. Switch persona to **Security Auditor**, scroll to **Audit logs**, click **Search**:
   - You'll see the business actions `create_vehicle` (actor `staff:manufacturing`) and `register_vehicle`
     (actor `vehicle:service:simulated-vehicle:<VIN>`), **plus the service-to-service plumbing**:
     `bootstrap_provisioned` (factory burns in the device credential), and `service_token_issued` events
     for each workload-to-workload call (device→vehicle-service, factory→identity-service,
     vehicle-service→audit-service).
   - **Click any row to expand the raw JSON.** The call-home events share one `correlation_id`, so you can
     trace the whole flow: `bootstrap_provisioned` → `service_token_issued` → `register_vehicle`.
   - You'll also see a one-time **`signing_key_generated`** event — identity-service's Ed25519 signing key.
     Restart identity-service (`docker compose restart identity-service`) and search again to see a **new
     key event** — i.e. a key/"certificate" roll captured in the audit log.

> What was demonstrated: a workload (the vehicle) authenticating with a factory credential, receiving a
> short-lived JWT, and registering — with **every service-to-service token issuance and the signing-key
> lifecycle** recorded and correlation-traceable. Every created vehicle is burned in and brought online the
> same way; the fleet starts empty.

---

## Flow 2 — Consumer claims the vehicle (passkey)

1. In **consumer-web**, enter username **`alice`** and click **Create passkey** (approve the WebAuthn
   prompt). You are now signed in (session cookie).
2. In **Claim a vehicle**, enter the **VIN** (`VIN-DEMO-0001`) and the **claim code** from Flow 1, then
   click **Claim as owner**.
   - `alice` is now the **owner**; the vehicle moves to `CLAIMED` / `OWNER_ASSIGNED`.
3. Select the vehicle under **My vehicles** to see all six state dimensions and your role.
4. (Staff-web, Security Auditor) Search audit logs → a `claim_vehicle` ALLOW event for `alice`.

---

## Flow 3 — Invite a driver, and a denied action

1. As **alice**, open the vehicle detail → **Invite a driver**. Enter username **`bob`**, role
   **`driver`**, click **Send invite**.
2. Click **Switch user** (top right). Enter **`bob`**, click **Create passkey** (approve prompt).
3. As **bob**, a **Pending invitations** card appears → click **Accept**. `bob` now has the `driver` role.
4. Select the vehicle. As a driver, `bob` can operate the vehicle:
   - **Unlock doors** → allowed ✓
   - **Start climate** → allowed ✓
   - **Start vehicle (high-risk)** → allowed ✓ **after a passkey step-up** (drivers may drive; the step-up
     ceremony is shown in Flow 4).
5. But a driver is **not a manager**. Under **Invite a driver**, have `bob` try to invite someone (e.g.
   `carol`):
   - **Denied** — "invite_driver requires owner or co-owner".
6. (Staff-web, Security Auditor) Search audit logs → an `invite_driver` **DENY** event for `bob` with the
   reason recorded.

> What was demonstrated: resource-scoped roles and least privilege — a driver can *operate* the vehicle
> (including the high-risk start, gated by step-up) but cannot *manage* who else has access; the denied
> attempt is audited.

---

## Flow 4 — Owner runs the high-risk command with step-up

1. Click **Switch user** → sign **in** as **`alice`** (click **Sign in**, approve the passkey prompt).
2. Open the vehicle → click **Start vehicle (high-risk)**.
   - The app detects step-up is stale → "passkey step-up required" → your browser prompts for the passkey
     again (**step-up**).
   - After a successful step-up, the command is **allowed** → `power_state = STARTED`.
3. (Staff-web, Security Auditor) Search audit logs → a `start_vehicle` **ALLOW** event for `alice` with
   `step_up_fresh: true` in metadata.

> What was demonstrated: passkey **step-up** gating a high-risk command, plus an idempotency key so a
> replayed request does not re-execute. The same step-up gate applies to a **driver** starting the vehicle
> (Flow 3) — role and step-up are checked independently.

---

## Flow 5 — Staff authorization (persona least-privilege)

In **staff-web**, the **Fleet** is visible to every persona, but each persona only sees the **one action it
is authorized for**. Switch personas and watch the action widget change:

| Persona | Sees | Can do |
|---|---|---|
| **Manufacturing Operator** | Fleet + **Create vehicle** | create vehicles |
| **Sales / Support** | Fleet + **Assign owner** | assign an owner to a vehicle |
| **Security Auditor** | Fleet + **Audit logs** | search the audit log |

Hiding the other widgets is purely a UI convenience — **authorization is still enforced server-side**. The
endpoints reject the wrong persona regardless of the UI, and every denial is itself audited
(`decision = DENY`). You can confirm directly, e.g. a non-manufacturing persona calling create:

```bash
# sales_support is NOT allowed to create vehicles -> 403, and the denial is audited
curl -i -X POST http://localhost:8082/staff/vehicles/create \
  -H 'X-Staff-Persona: sales_support' -H 'Content-Type: application/json' -d '{}'

# a non-auditor persona cannot read the audit log -> 403
curl -i 'http://localhost:8083/audit/search' -H 'X-Staff-Persona: manufacturing'
```

---

## Reset

```bash
make down     # stop and remove containers + volumes (fresh DB next time)
```
