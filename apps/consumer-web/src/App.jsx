import React, { useEffect, useState } from 'react'
import * as api from './api.js'

const DIMENSIONS = [
  'lifecycle_status', 'ownership_state', 'access_state',
  'power_state', 'climate_state', 'connectivity_state',
]

export default function App() {
  const [user, setUser] = useState(null)
  const [stepUpFresh, setStepUpFresh] = useState(false)
  const [booted, setBooted] = useState(false)

  async function refreshMe() {
    try {
      const m = await api.me()
      setUser(m.user)
      setStepUpFresh(m.step_up_fresh)
      return m
    } catch {
      setUser(null)
      setStepUpFresh(false)
      return null
    }
  }

  useEffect(() => { refreshMe().finally(() => setBooted(true)) }, [])

  if (!booted) return <div className="container">Loading…</div>

  return (
    <div className="container">
      <header>
        <h1>🔑 Vehicle Identity — Consumer</h1>
        {user && (
          <div className="who">
            Signed in as <b>{user.username}</b>{' '}
            <span className={stepUpFresh ? 'badge ok' : 'badge'}>
              step-up {stepUpFresh ? 'fresh' : 'stale'}
            </span>{' '}
            <button onClick={() => setUser(null)}>Switch user</button>
          </div>
        )}
      </header>
      {!user ? <Auth onAuthed={refreshMe} /> : <Dashboard user={user} refreshMe={refreshMe} />}
    </div>
  )
}

function Auth({ onAuthed }) {
  const [username, setUsername] = useState('')
  const [msg, setMsg] = useState(null)
  const [busy, setBusy] = useState(false)

  async function run(fn) {
    setMsg(null); setBusy(true)
    try {
      await fn(username.trim())
      await onAuthed()
    } catch (e) {
      setMsg({ type: 'err', text: e.message })
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="card">
      <h2>Sign up / Sign in with a passkey</h2>
      <p className="muted">Real WebAuthn — your browser/OS will prompt for a passkey.</p>
      <input
        placeholder="username (e.g. alice)"
        value={username}
        onChange={(e) => setUsername(e.target.value)}
      />
      <div className="row">
        <button disabled={busy || !username} onClick={() => run(api.signup)}>Create passkey</button>
        <button disabled={busy || !username} onClick={() => run(api.signin)}>Sign in</button>
      </div>
      {msg && <Notice {...msg} />}
    </div>
  )
}

function Dashboard({ user, refreshMe }) {
  const [vehicles, setVehicles] = useState([])
  const [invitations, setInvitations] = useState([])
  const [selected, setSelected] = useState(null)
  const [notice, setNotice] = useState(null)

  async function refresh() {
    try {
      const [v, inv] = await Promise.all([api.listVehicles(), api.listInvitations()])
      setVehicles(v.vehicles || [])
      setInvitations(inv.invitations || [])
    } catch (e) {
      setNotice({ type: 'err', text: e.message })
    }
  }

  useEffect(() => { refresh() }, [user.username])

  return (
    <div className="grid">
      <div>
        <Claim onDone={(t) => { setNotice(t); refresh() }} />
        <Invitations invitations={invitations} onDone={(t) => { setNotice(t); refresh() }} />
        <MyVehicles vehicles={vehicles} onSelect={setSelected} selected={selected} />
      </div>
      <div>
        {notice && <Notice {...notice} />}
        {selected
          ? <VehicleDetail
              id={selected}
              user={user}
              refreshMe={refreshMe}
              onNotice={setNotice}
              onChanged={refresh} />
          : <div className="card muted">Select a vehicle to view status and send commands.</div>}
      </div>
    </div>
  )
}

function Claim({ onDone }) {
  const [vin, setVin] = useState('')
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)

  async function doClaim() {
    setBusy(true)
    try {
      const found = await api.lookupVin(vin.trim())
      await api.claim(found.id, code.trim())
      onDone({ type: 'ok', text: `Claimed ${vin} — you are now the owner.` })
      setVin(''); setCode('')
    } catch (e) {
      onDone({ type: 'err', text: `Claim failed: ${e.message}` })
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="card">
      <h2>Claim a vehicle</h2>
      <input placeholder="VIN" value={vin} onChange={(e) => setVin(e.target.value)} />
      <input placeholder="claim code" value={code} onChange={(e) => setCode(e.target.value)} />
      <button disabled={busy || !vin || !code} onClick={doClaim}>Claim as owner</button>
    </div>
  )
}

function Invitations({ invitations, onDone }) {
  if (!invitations.length) return null
  async function accept(code) {
    try {
      await api.acceptInvite(code)
      onDone({ type: 'ok', text: 'Invitation accepted — role granted.' })
    } catch (e) {
      onDone({ type: 'err', text: e.message })
    }
  }
  return (
    <div className="card">
      <h2>Pending invitations</h2>
      {invitations.map((i) => (
        <div key={i.code} className="listrow">
          <span><b>{i.vin}</b> as <b>{i.role}</b> <span className="muted">from {i.invited_by}</span></span>
          <button onClick={() => accept(i.code)}>Accept</button>
        </div>
      ))}
    </div>
  )
}

function MyVehicles({ vehicles, onSelect, selected }) {
  return (
    <div className="card">
      <h2>My vehicles</h2>
      {vehicles.length === 0 && <p className="muted">No vehicles yet. Claim one or accept an invite.</p>}
      {vehicles.map((v) => (
        <div
          key={v.id}
          className={'listrow clickable' + (selected === v.id ? ' active' : '')}
          onClick={() => onSelect(v.id)}>
          <span><b>{v.vin}</b> <span className="muted">{v.model}</span></span>
          <span className="badge">{v.lifecycle_status}</span>
        </div>
      ))}
    </div>
  )
}

function VehicleDetail({ id, user, refreshMe, onNotice, onChanged }) {
  const [data, setData] = useState(null)
  const [busy, setBusy] = useState(false)

  async function load() {
    try { setData(await api.getVehicle(id)) }
    catch (e) { onNotice({ type: 'err', text: e.message }); setData(null) }
  }
  useEffect(() => { load() }, [id])

  async function cmd(label, fn) {
    setBusy(true); onNotice(null)
    try {
      await fn()
      onNotice({ type: 'ok', text: `${label}: allowed ✓` })
      await load(); onChanged()
    } catch (e) {
      onNotice({ type: 'err', text: `${label}: denied — ${e.message}` })
    } finally {
      setBusy(false)
    }
  }

  async function startVehicle() {
    setBusy(true); onNotice(null)
    try {
      const key = crypto.randomUUID()
      try {
        await api.startVehicleCmd(id, key)
      } catch (e) {
        if (e.status === 428) {
          onNotice({ type: 'info', text: 'High-risk command: passkey step-up required…' })
          await api.stepUp()
          await refreshMe()
          await api.startVehicleCmd(id, crypto.randomUUID())
        } else {
          throw e
        }
      }
      onNotice({ type: 'ok', text: 'Start vehicle: allowed ✓ (power STARTED)' })
      await load(); await refreshMe(); onChanged()
    } catch (e) {
      onNotice({ type: 'err', text: `Start vehicle: denied — ${e.message}` })
    } finally {
      setBusy(false)
    }
  }

  if (!data) return <div className="card">Loading vehicle…</div>
  const v = data.vehicle
  const role = data.your_role || '(none)'

  return (
    <div className="card">
      <h2>{v.vin} <span className="muted">{v.model}</span></h2>
      <p>Your role: <span className="badge ok">{role}</span></p>
      <table className="states">
        <tbody>
          {DIMENSIONS.map((d) => (
            <tr key={d}><td className="muted">{d}</td><td><b>{v[d]}</b></td></tr>
          ))}
        </tbody>
      </table>

      <h3>Commands</h3>
      <div className="row wrap">
        <button disabled={busy} onClick={() => cmd('Unlock doors', () => api.unlock(id))}>Unlock doors</button>
        <button disabled={busy} onClick={() => cmd('Start climate', () => api.startClimate(id, 'AUTO'))}>Start climate</button>
        <button disabled={busy} className="danger" onClick={startVehicle}>Start vehicle (high-risk)</button>
      </div>

      <h3>Invite a driver</h3>
      <InviteForm id={id} onNotice={onNotice} onChanged={onChanged} />

      {data.grants && data.grants.length > 0 && (
        <>
          <h3>People</h3>
          {data.grants.map((g) => (
            <div key={g.username} className="listrow">
              <span>{g.username}</span><span className="badge">{g.role}</span>
            </div>
          ))}
        </>
      )}
    </div>
  )
}

function InviteForm({ id, onNotice, onChanged }) {
  const [username, setUsername] = useState('')
  const [role, setRole] = useState('driver')
  const [busy, setBusy] = useState(false)

  async function send() {
    setBusy(true)
    try {
      const inv = await api.invite(id, username.trim(), role)
      onNotice({ type: 'ok', text: `Invited ${username} as ${role}. They can accept after signing in. (code ${inv.code})` })
      setUsername(''); onChanged()
    } catch (e) {
      onNotice({ type: 'err', text: `Invite failed: ${e.message}` })
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="row wrap">
      <input placeholder="username to invite" value={username} onChange={(e) => setUsername(e.target.value)} />
      <select value={role} onChange={(e) => setRole(e.target.value)}>
        <option value="driver">driver</option>
        <option value="co-owner">co-owner</option>
        <option value="viewer">viewer</option>
      </select>
      <button disabled={busy || !username} onClick={send}>Send invite</button>
    </div>
  )
}

function Notice({ type, text }) {
  return <div className={'notice ' + type}>{text}</div>
}
