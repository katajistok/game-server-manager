import { state } from './state.js'
import { escapeHtml, parseJSON } from './utils.js'

export async function loadAdminData() {
  const [pending, defs, agentList] = await Promise.all([
    fetch('/api/agents/pending').then(r => r.json()),
    fetch('/api/game-definitions').then(r => r.json()),
    fetch('/api/agents').then(r => r.json()),
  ])
  state.gameDefs = defs
  renderPendingAgents(pending)
  renderAgentSelect(agentList)
  renderGameDefSelect(defs)
}

function renderPendingAgents(pending) {
  const el = document.getElementById('pending-agents-list')
  if (!pending.length) {
    el.innerHTML = '<div class="status-text">No pending agents</div>'
    return
  }
  el.innerHTML = pending.map(a => `
    <div class="pending-agent-row">
      <div>
        <div class="pending-agent-name">${escapeHtml(a.name)}</div>
        <div class="pending-agent-meta">
          ${a.connected ? '🟢 Connected and waiting' : '⚫ Disconnected'}
          ${a.host_info?.os        ? ' · ' + a.host_info.os        : ''}
          ${a.host_info?.cpu_cores ? ' · ' + a.host_info.cpu_cores + ' cores' : ''}
        </div>
      </div>
      <div class="pending-agent-actions">
        <button class="btn btn-green" onclick="approveAgent(${a.id}, '${escapeHtml(a.name)}')">✓ Approve</button>
        <button class="btn btn-red"   onclick="deleteAgent(${a.id})">✕ Reject</button>
      </div>
    </div>
  `).join('')
}

function renderAgentSelect(agentList) {
  const el = document.getElementById('new-server-agent')
  const approved = agentList.filter(a => a.status === 'online' || a.status === 'approved')
  el.innerHTML = approved.length
    ? approved.map(a => `<option value="${a.id}">${escapeHtml(a.name)}</option>`).join('')
    : '<option value="">No approved agents</option>'
}

function renderGameDefSelect(defs) {
  const el = document.getElementById('new-server-game')
  el.innerHTML = defs.map(d =>
    `<option value="${d.id}">${escapeHtml(d.name)}</option>`
  ).join('')
  updateModeSelect()
  updateFormDefaults()
}

export function updateModeSelect() {
  const gameID = parseInt(document.getElementById('new-server-game').value)
  const def    = state.gameDefs.find(d => d.id === gameID)
  const el     = document.getElementById('new-server-mode')
  el.innerHTML = (def?.modes || []).map(m =>
    `<option value="${m.id}">${escapeHtml(m.name)}</option>`
  ).join('')
}

export function updateFormDefaults() {
  const gameID = parseInt(document.getElementById('new-server-game').value)
  const def    = state.gameDefs.find(d => d.id === gameID)
  if (!def) return
  document.getElementById('new-server-port').placeholder       = def.default_port
  document.getElementById('new-server-maxplayers').value       = def.default_max_players
  renderCustomFields(def)
}

function renderCustomFields(def) {
  const fields    = parseJSON(def?.custom_fields, [])
  const container = document.getElementById('new-server-custom-fields')
  container.innerHTML = fields.map(f => `
    <div class="form-field">
      <label class="form-label">${escapeHtml(f.label)}</label>
      <input class="form-input" id="new-cf-${escapeHtml(f.key)}"
             placeholder="${escapeHtml(f.placeholder || '')}"
             type="${f.type === 'password' ? 'password' : 'text'}" />
    </div>
  `).join('')
}

export async function approveAgent(id, name) {
  try {
    const res  = await fetch(`/api/agents/${id}/approve`, { method: 'POST' })
    const data = await res.json()
    if (data.token) {
      document.getElementById('token-modal-input').value = data.token
      document.getElementById('token-modal-copied').textContent = ''
      document.getElementById('token-modal').style.display = 'flex'
    }
    loadAdminData()
    window.loadData && window.loadData()
  } catch(e) {
    window.showToast('Failed to approve agent', 'error')
  }
}

export async function deleteAgent(id) {
  if (!await window.showConfirm('Reject and delete this agent?', 'Reject Agent')) return
  await fetch(`/api/agents/${id}`, { method: 'DELETE' })
  loadAdminData()
  window.loadData && window.loadData()
}

export async function createServer() {
  const name         = document.getElementById('new-server-name').value.trim()
  const agentID      = parseInt(document.getElementById('new-server-agent').value)
  const gameDefID    = parseInt(document.getElementById('new-server-game').value)
  const gameModeID   = parseInt(document.getElementById('new-server-mode').value) || null
  const port         = parseInt(document.getElementById('new-server-port').value)
  const maxPlayers   = parseInt(document.getElementById('new-server-maxplayers').value)
  const password     = document.getElementById('new-server-password').value.trim()
  const rconPassword = document.getElementById('new-server-rcon').value.trim()

  if (!name || !agentID || !gameDefID || !port) {
    window.showToast('Please fill in Server Name, Agent, Game and Port', 'error')
    return
  }

  const def    = state.gameDefs.find(d => d.id === gameDefID)
  const fields = parseJSON(def?.custom_fields, [])
  const customConfig = {}
  fields.forEach(f => {
    const el = document.getElementById(`new-cf-${f.key}`)
    if (el && el.value.trim()) customConfig[f.key] = el.value.trim()
  })

  try {
    const res = await fetch('/api/servers', {
      method:  'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        agent_id: agentID, game_definition_id: gameDefID, game_mode_id: gameModeID,
        name, port, max_players: maxPlayers,
        password, rcon_password: rconPassword,
        custom_config: customConfig,
      }),
    })
    const data = await res.json()
    if (data.id) {
      document.getElementById('new-server-name').value     = ''
      document.getElementById('new-server-password').value = ''
      document.getElementById('new-server-rcon').value     = ''
      document.querySelectorAll('#new-server-custom-fields input').forEach(el => { el.value = '' })
      window.loadData && window.loadData()
      window.switchMainView && window.switchMainView('dashboard')
      window.showToast('Server created successfully!', 'success')
    } else {
      window.showToast('Error: ' + (data.error || 'unknown error'), 'error')
    }
  } catch(e) {
    window.showToast('Failed to create server', 'error')
  }
}
