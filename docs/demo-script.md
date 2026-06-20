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
   - You'll see `create_vehicle` (actor `staff:manufacturing`, ALLOW) and `register_vehicle`
     (actor `vehicle:service:simulated-vehicle:<VIN>`, ALLOW) sharing a correlation trail.

> What was demonstrated: a workload (the vehicle) authenticating with a factory credential, receiving a
> short-lived JWT, and registering — all audited. This works for **any** created vehicle, not just the
> seeded demo VIN.

If you prefer to skip the manual step, run `make seed` instead (creates the demo vehicle for you).

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

## Flow 3 — Invite a driver, then a denied command

1. As **alice**, open the vehicle detail → **Invite a driver**. Enter username **`bob`**, role
   **`driver`**, click **Send invite**.
2. Click **Switch user** (top right). Enter **`bob`**, click **Create passkey** (approve prompt).
3. As **bob**, a **Pending invitations** card appears → click **Accept**. `bob` now has the `driver` role.
4. Select the vehicle. As a driver, `bob` can:
   - **Unlock doors** → allowed ✓
   - **Start climate** → allowed ✓
5. Click **Start vehicle (high-risk)**:
   - **Denied** — "start_vehicle requires owner or co-owner". (No step-up prompt; the role check fails
     first.)
6. (Staff-web, Security Auditor) Search audit logs → a `start_vehicle` **DENY** event for `bob` with the
   reason recorded.

> What was demonstrated: resource-scoped roles, least privilege, and that **denied** high-risk attempts are
> audited.

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
> replayed request does not re-execute.

---

## Flow 5 — Staff authorization (persona least-privilege)

In **staff-web**, switch personas and observe server-side enforcement:

| Persona | Try | Expected |
|---|---|---|
| **Service Technician** | View Fleet / a vehicle's status | Allowed (read only); cannot unlock/start/assign |
| **Sales / Support** | **Assign owner** (paste a vehicle id + a username) | Allowed |
| **Sales / Support** | **Create vehicle** | **Denied** |
| **Manufacturing** | **Create vehicle** | Allowed |
| **Manufacturing** | **Assign owner** | **Denied** |
| **Security Auditor** | **Audit logs → Search** | Allowed |
| Any non-auditor | **Audit logs → Search** | **Denied** (403) |

Each denial is itself written to the audit log (action with `decision = DENY`).

---

## Reset

```bash
make down     # stop and remove containers + volumes (fresh DB next time)
```
