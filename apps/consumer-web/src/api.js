// API + WebAuthn client for the consumer web app.

const IDENTITY = import.meta.env.VITE_IDENTITY_URL || 'http://localhost:8081'
const VEHICLE = import.meta.env.VITE_VEHICLE_URL || 'http://localhost:8082'

// ---- low-level fetch ----

async function request(method, url, body) {
  const opts = {
    method,
    credentials: 'include',
    headers: {},
  }
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
    err.data = data
    throw err
  }
  return data
}

const get = (url) => request('GET', url)
const post = (url, body) => request('POST', url, body)

// ---- WebAuthn helpers ----

function b64urlToBuf(s) {
  s = s.replace(/-/g, '+').replace(/_/g, '/')
  const pad = s.length % 4
  if (pad) s += '='.repeat(4 - pad)
  const bin = atob(s)
  const buf = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) buf[i] = bin.charCodeAt(i)
  return buf.buffer
}

function bufToB64url(buf) {
  const bytes = new Uint8Array(buf)
  let bin = ''
  for (const b of bytes) bin += String.fromCharCode(b)
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

function preformatCreate(pk) {
  pk.challenge = b64urlToBuf(pk.challenge)
  pk.user.id = b64urlToBuf(pk.user.id)
  if (pk.excludeCredentials) {
    pk.excludeCredentials = pk.excludeCredentials.map((c) => ({ ...c, id: b64urlToBuf(c.id) }))
  }
  return pk
}

function preformatGet(pk) {
  pk.challenge = b64urlToBuf(pk.challenge)
  if (pk.allowCredentials) {
    pk.allowCredentials = pk.allowCredentials.map((c) => ({ ...c, id: b64urlToBuf(c.id) }))
  }
  return pk
}

function serializeAttestation(cred) {
  return {
    id: cred.id,
    rawId: bufToB64url(cred.rawId),
    type: cred.type,
    response: {
      clientDataJSON: bufToB64url(cred.response.clientDataJSON),
      attestationObject: bufToB64url(cred.response.attestationObject),
    },
    clientExtensionResults: cred.getClientExtensionResults ? cred.getClientExtensionResults() : {},
  }
}

function serializeAssertion(cred) {
  return {
    id: cred.id,
    rawId: bufToB64url(cred.rawId),
    type: cred.type,
    response: {
      clientDataJSON: bufToB64url(cred.response.clientDataJSON),
      authenticatorData: bufToB64url(cred.response.authenticatorData),
      signature: bufToB64url(cred.response.signature),
      userHandle: cred.response.userHandle ? bufToB64url(cred.response.userHandle) : null,
    },
    clientExtensionResults: cred.getClientExtensionResults ? cred.getClientExtensionResults() : {},
  }
}

// ---- auth ceremonies (real WebAuthn) ----

export async function signup(username) {
  const start = await post(`${IDENTITY}/signup/start`, { username })
  const pk = preformatCreate(start.options.publicKey)
  const cred = await navigator.credentials.create({ publicKey: pk })
  const credential = serializeAttestation(cred)
  return post(`${IDENTITY}/signup/finish`, { ceremony_id: start.ceremony_id, credential })
}

export async function signin(username) {
  const start = await post(`${IDENTITY}/signin/start`, { username })
  const pk = preformatGet(start.options.publicKey)
  const cred = await navigator.credentials.get({ publicKey: pk })
  const credential = serializeAssertion(cred)
  return post(`${IDENTITY}/signin/finish`, { ceremony_id: start.ceremony_id, credential })
}

export async function stepUp() {
  const start = await post(`${IDENTITY}/step-up/start`, {})
  const pk = preformatGet(start.options.publicKey)
  const cred = await navigator.credentials.get({ publicKey: pk })
  const credential = serializeAssertion(cred)
  return post(`${IDENTITY}/step-up/finish`, { ceremony_id: start.ceremony_id, credential })
}

export const me = () => get(`${IDENTITY}/me`)

// ---- vehicle API ----

export const listVehicles = () => get(`${VEHICLE}/vehicles`)
export const lookupVin = (vin) => get(`${VEHICLE}/vehicles/lookup?vin=${encodeURIComponent(vin)}`)
export const getVehicle = (id) => get(`${VEHICLE}/vehicles/${id}`)
export const claim = (id, claimCode) => post(`${VEHICLE}/vehicles/${id}/claim`, { claim_code: claimCode })
export const invite = (id, username, role) => post(`${VEHICLE}/vehicles/${id}/invite`, { username, role })
export const listInvitations = () => get(`${VEHICLE}/invitations`)
export const acceptInvite = (code) => post(`${VEHICLE}/invitations/${code}/accept`, {})
export const unlock = (id) => post(`${VEHICLE}/vehicles/${id}/commands/unlock`, {})
export const startClimate = (id, mode) => post(`${VEHICLE}/vehicles/${id}/commands/start-climate`, { mode })
export const startVehicleCmd = (id, key) => post(`${VEHICLE}/vehicles/${id}/commands/start-vehicle`, { idempotency_key: key })
