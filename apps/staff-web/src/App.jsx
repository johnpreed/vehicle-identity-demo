import React, { useEffect, useState } from 'react'
import * as api from './api.js'

const PERSONAS = [
  { id: 'manufacturing', label: 'Manufacturing Operator', can: 'spawn_vehicle' },
  { id: 'sales_support', label: 'Sales / Support', can: 'assign_owner' },
  { id: 'service_technician', label: 'Service Technician', can: 'view_status' },
  { id: 'security_auditor', label: 'Security Auditor', can: 'read_audit_logs' },
]

export default function App() {
  const [persona, setPersona] = useState('manufacturing')
  const [notice, setNotice] = useState(null)

  return (
    <div className="container">
      <header>
        <h1>🏭 Vehicle Identity — Staff Console</h1>
      </header>

      <div className="card">
        <h2>Persona</h2>
        <p className="muted">
          Switch persona to see server-side authorization in action. Authorization is enforced by
          vehicle-service / audit-service, not by this UI.
        </p>
        <div className="row wrap">
          {PERSONAS.map((p) => (
            <button
              key={p.id}
              className={'persona' + (persona === p.id ? ' active' : '')}
              onClick={() => { setPersona(p.id); setNotice(null) }}>
              {p.label}
              <span className="cap">can: {p.can}</span>
            </button>
          ))}
        </div>
      </div>

      {notice && <div className={'notice ' + notice.type}>{notice.text}</div>}

      <div className="grid">
        <div>
          <Spawn persona={persona} onNotice={setNotice} />
          <AssignOwner persona={persona} onNotice={setNotice} />
        </div>
        <div>
          <Vehicles persona={persona} onNotice={setNotice} />
        </div>
      </div>

      <AuditLogs persona={persona} onNotice={setNotice} />
    </div>
  )
}

function Spawn({ persona, onNotice }) {
  const [vin, setVin] = useState('')
  const [model, setModel] = useState('Demo EV')
  const [busy, setBusy] = useState(false)
  async function run() {
    setBusy(true)
    try {
      const v = await api.spawn(persona, vin.trim(), model.trim())
      onNotice({ type: 'ok', text: `Spawned ${v.vin} (claim code ${v.claim_code}) as ${persona}.` })
      setVin('')
    } catch (e) {
      onNotice({ type: 'err', text: `Spawn denied (${persona}): ${e.message}` })
    } finally { setBusy(false) }
  }
  return (
    <div className="card">
      <h2>Spawn vehicle <span className="muted">(manufacturing)</span></h2>
      <input placeholder="VIN (blank = auto)" value={vin} onChange={(e) => setVin(e.target.value)} />
      <input placeholder="model" value={model} onChange={(e) => setModel(e.target.value)} />
      <button disabled={busy} onClick={run}>Spawn</button>
    </div>
  )
}

function AssignOwner({ persona, onNotice }) {
  const [id, setId] = useState('')
  const [username, setUsername] = useState('')
  const [busy, setBusy] = useState(false)
  async function run() {
    setBusy(true)
    try {
      await api.assignOwner(persona, id.trim(), username.trim())
      onNotice({ type: 'ok', text: `Assigned ${username} as owner of ${id}.` })
      setUsername('')
    } catch (e) {
      onNotice({ type: 'err', text: `Assign owner denied (${persona}): ${e.message}` })
    } finally { setBusy(false) }
  }
  return (
    <div className="card">
      <h2>Assign owner <span className="muted">(sales_support)</span></h2>
      <input placeholder="vehicle id" value={id} onChange={(e) => setId(e.target.value)} />
      <input placeholder="username" value={username} onChange={(e) => setUsername(e.target.value)} />
      <button disabled={busy || !id || !username} onClick={run}>Assign owner</button>
    </div>
  )
}

function Vehicles({ persona, onNotice }) {
  const [vehicles, setVehicles] = useState([])
  async function load() {
    try {
      const v = await api.listVehicles(persona)
      setVehicles(v.vehicles || [])
    } catch (e) {
      onNotice({ type: 'err', text: `List vehicles failed: ${e.message}` })
    }
  }
  useEffect(() => { load() }, [persona])
  return (
    <div className="card">
      <h2>Fleet <button className="ghost" onClick={load}>↻</button></h2>
      {vehicles.length === 0 && <p className="muted">No vehicles. Spawn one as manufacturing.</p>}
      <table className="states">
        <thead><tr><th>VIN</th><th>id</th><th>lifecycle</th><th>claim code</th><th>conn.</th></tr></thead>
        <tbody>
          {vehicles.map((v) => (
            <tr key={v.id}>
              <td><b>{v.vin}</b></td>
              <td className="muted mono small">{v.id.slice(0, 8)}…</td>
              <td><span className="badge">{v.lifecycle_status}</span></td>
              <td className="mono">{v.claim_code || '—'}</td>
              <td className="muted">{v.connectivity_state}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function AuditLogs({ persona, onNotice }) {
  const [rows, setRows] = useState([])
  const [resourceId, setResourceId] = useState('')
  const [loaded, setLoaded] = useState(false)
  async function load() {
    try {
      const r = await api.searchAudit(persona, { resource_id: resourceId.trim(), limit: 100 })
      setRows(r.results || [])
      setLoaded(true)
    } catch (e) {
      setRows([])
      setLoaded(true)
      onNotice({ type: 'err', text: `Audit search denied (${persona}): ${e.message}` })
    }
  }
  return (
    <div className="card">
      <h2>Audit logs <span className="muted">(security_auditor)</span></h2>
      <div className="row wrap">
        <input placeholder="filter by vehicle id (optional)" value={resourceId} onChange={(e) => setResourceId(e.target.value)} />
        <button onClick={load}>Search</button>
      </div>
      {loaded && rows.length === 0 && <p className="muted">No results (or access denied for this persona).</p>}
      {rows.length > 0 && (
        <table className="states">
          <thead><tr><th>time</th><th>actor</th><th>action</th><th>decision</th><th>reason</th><th>corr.</th></tr></thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.id}>
                <td className="muted small">{new Date(r.created_at).toLocaleTimeString()}</td>
                <td className="small">{r.actor_type}:{r.actor_id}</td>
                <td className="mono small">{r.action}</td>
                <td><span className={'badge ' + (r.decision === 'ALLOW' ? 'ok' : 'deny')}>{r.decision}</span></td>
                <td className="muted small">{r.reason}</td>
                <td className="muted mono small">{(r.correlation_id || '').slice(0, 8)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
