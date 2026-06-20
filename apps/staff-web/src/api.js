// Staff API client. Every request carries the selected persona via X-Staff-Persona.
// Staff auth is intentionally a demo-only persona header (no passkey) — authorization
// is enforced server-side by vehicle-service and audit-service.

const VEHICLE = import.meta.env.VITE_VEHICLE_URL || 'http://localhost:8082'
const AUDIT = import.meta.env.VITE_AUDIT_URL || 'http://localhost:8083'

async function request(method, url, persona, body) {
  const opts = { method, headers: { 'X-Staff-Persona': persona } }
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json'
    opts.body = JSON.stringify(body)
  }
  const resp = await fetch(url, opts)
  const text = await resp.text()
  let data = null
  try { data = text ? JSON.parse(text) : null } catch { data = { raw: text } }
  if (!resp.ok) {
    const err = new Error((data && data.error) || resp.statusText)
    err.status = resp.status
    throw err
  }
  return data
}

export const listVehicles = (persona) => request('GET', `${VEHICLE}/vehicles`, persona)
export const createVehicle = (persona, vin, model) =>
  request('POST', `${VEHICLE}/staff/vehicles/create`, persona, { vin, model })
export const assignOwner = (persona, id, username) =>
  request('POST', `${VEHICLE}/staff/vehicles/${id}/assign-owner`, persona, { username })

export function searchAudit(persona, filters = {}) {
  const qs = new URLSearchParams(Object.entries(filters).filter(([, v]) => v)).toString()
  return request('GET', `${AUDIT}/audit/search${qs ? '?' + qs : ''}`, persona)
}
