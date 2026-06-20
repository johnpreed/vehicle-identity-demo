#!/usr/bin/env bash
# Convenience: create a demo vehicle (manufacturing persona) so you have something to
# claim without driving the staff UI first. The simulated-vehicle fleet then burns in a
# bootstrap credential for it and registers it automatically. Pass a VIN as $1 to override.
set -euo pipefail

VEHICLE_URL="${VEHICLE_URL_HOST:-http://localhost:8082}"
VIN="${1:-VIN-DEMO-0001}"

echo "Creating demo vehicle ${VIN} via ${VEHICLE_URL} (manufacturing persona)..."
curl -fsS -X POST "${VEHICLE_URL}/staff/vehicles/create" \
  -H 'Content-Type: application/json' \
  -H 'X-Staff-Persona: manufacturing' \
  -d "{\"vin\":\"${VIN}\",\"model\":\"Demo EV\"}" || {
    echo "Create failed (vehicle may already exist). Continuing."
  }
echo
echo "Done. The simulated vehicle will burn in a credential and register it within a few seconds."
