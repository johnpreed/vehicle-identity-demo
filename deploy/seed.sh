#!/usr/bin/env bash
# Seed convenience: spawn the demo vehicle (manufacturing persona) so the simulated
# vehicle can register without manually driving the staff UI first.
set -euo pipefail

VEHICLE_URL="${VEHICLE_URL_HOST:-http://localhost:8082}"
VIN="${SIM_VIN:-}"
if [ -z "${VIN}" ] && [ -f .env ]; then
  VIN="$(grep -E '^SIM_VIN=' .env | head -1 | cut -d= -f2- | tr -d '"')"
fi
VIN="${VIN:-VIN-DEMO-0001}"

echo "Spawning demo vehicle ${VIN} via ${VEHICLE_URL} (manufacturing persona)..."
curl -fsS -X POST "${VEHICLE_URL}/staff/vehicles/spawn" \
  -H 'Content-Type: application/json' \
  -H 'X-Staff-Persona: manufacturing' \
  -d "{\"vin\":\"${VIN}\",\"model\":\"Demo EV\"}" || {
    echo "Spawn failed (vehicle may already exist). Continuing."
  }
echo
echo "Done. The simulated vehicle will call home and register within a few seconds."
