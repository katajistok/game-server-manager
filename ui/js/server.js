import { state } from './state.js'
import { escapeHtml, formatMB, drawSparkline, parseJSON } from './utils.js'
import { wsSend } from './ws.js'
import { renderAgents, renderServers } from './sidebar.js'
import { showPanel } from './agent.js'
import { getGame } from './game-registry.js'

export async function openServer(id) {
  state.selectedServer = state.servers.find(s => s.id === id)
  state.selectedAgent = null
  if (!state.selectedServer) return

  renderAgents()
  renderServers()
  showPanel('server-panel')
  updateServerHeader(state.selectedServer)
  switchTab('logs')
  clearLogs()

  const rconGame = getGame(state.selectedServer.game_slug)
  const rconInput = document.getElementById('rcon-input')
  if (rconInput && rconGame.rconPlaceholder) rconInput.placeholder = rconGame.rconPlaceholder

  try {
    const logs = await fetch(`/api/servers/${id}/logs`).then(r => r.json())
    logs.reverse().forEach(l => appendLog(l.line, l.logged_at))
  } catch(e) {
    console.error('log load error', e)
  }

  wsSend({ type: 'stream_logs_start', payload: {
    server_id: id,
    container_name: state.selectedServer.container_name,
  }})
}

export function closeServer() {
  if (state.selectedServer) {
    wsSend({ type: 'stream_logs_stop', payload: { server_id: state.selectedServer.id }})
  }
  state.selectedServer = null
  state.selectedAgent  = null
  renderAgents()
  renderServers()
  showPanel('empty-state')
}

export function updateServerHeader(server) {
  state.selectedServer = server
  document.getElementById('server-title').textContent = server.name
  document.getElementById('server-subtitle').textContent =
    `${server.agent_name} · port ${server.port} · max ${server.max_players} players` +
    (server.has_password ? ' · 🔒 password protected' : '')
  const badge = document.getElementById('server-status-badge')
  badge.textContent = server.status
  badge.className   = `badge badge-${server.status}`
  document.getElementById('btn-start').disabled =
    server.status === 'running' || server.status === 'starting'
  document.getElementById('btn-stop').disabled  =
    server.status === 'stopped' || server.status === 'stopping'
}

export function switchTab(tab) {
  document.querySelectorAll('.tab-btn').forEach((btn, i) => {
    btn.classList.toggle('active', ['logs', 'rcon', 'metrics', 'config'][i] === tab)
  })
  document.querySelectorAll('.tab-panel').forEach(p => {
    p.classList.toggle('active', p.id === `tab-${tab}`)
  })
  if (tab === 'metrics' && state.selectedServer) renderContainerMetrics(state.selectedServer.container_name)
  if (tab === 'config'  && state.selectedServer) openServerConfig()
}

export function clearLogs() {
  document.getElementById('log-output').innerHTML =
    '<div class="log-empty">No logs yet. Start the server to see output.</div>'
}

export function appendLog(line, time) {
  const el = document.getElementById('log-output')
  const empty = el.querySelector('.log-empty')
  if (empty) empty.remove()
  const div = document.createElement('div')
  div.className = 'log-line'
  div.innerHTML = `<span class="log-time">${time ? new Date(time).toLocaleTimeString() : ''}</span>${escapeHtml(line)}`
  el.appendChild(div)
  while (el.children.length > 500) el.removeChild(el.firstChild)
  el.scrollTop = el.scrollHeight
}

export function appendRcon(type, text) {
  const el = document.getElementById('rcon-output')
  const empty = el.querySelector('.rcon-empty')
  if (empty) empty.remove()
  const div = document.createElement('div')
  if (type === 'command') {
    div.className = 'rcon-command'
    div.innerHTML = `<span class="rcon-command-prefix">></span>${escapeHtml(text)}`
  } else if (type === 'error') {
    div.className = 'rcon-error'
    div.textContent = text
  } else {
    div.className = 'rcon-response'
    div.textContent = text
  }
  el.appendChild(div)
  el.scrollTop = el.scrollHeight
}

export function handleRconKey(e) {
  if (e.key === 'Enter') sendRcon()
}

export async function sendRcon() {
  const input = document.getElementById('rcon-input')
  const cmd = input.value.trim()
  if (!cmd || !state.selectedServer) return
  input.value = ''
  appendRcon('command', cmd)
  try {
    await fetch(`/api/servers/${state.selectedServer.id}/rcon`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ command: cmd }),
    })
  } catch(e) {
    appendRcon('error', 'Failed to send command')
  }
}

export function renderContainerMetrics(containerName) {
  const history = state.containerMetricsHistory[containerName] || []
  const el = document.getElementById('container-metrics-content')
  if (!history.length) {
    el.innerHTML = '<div class="metrics-empty">No metrics yet. Metrics update every 30 seconds.</div>'
    return
  }
  const latest = history[history.length - 1]
  const cpuHistory = history.map(h => h.cpu_percent)
  const memHistory = history.map(h => h.mem_limit_mb > 0 ? (h.mem_used_mb / h.mem_limit_mb) * 100 : 0)
  el.innerHTML = `
    <div class="container-metrics-grid">
      <div class="card">
        <div class="metric-label">CPU Usage</div>
        <div class="metric-value indigo">${latest.cpu_percent.toFixed(1)}%</div>
        <canvas id="sparkline-container-cpu" class="sparkline"></canvas>
      </div>
      <div class="card">
        <div class="metric-label">Memory Usage</div>
        <div class="metric-value green">${formatMB(latest.mem_used_mb)} / ${formatMB(latest.mem_limit_mb)}</div>
        <canvas id="sparkline-container-mem" class="sparkline"></canvas>
      </div>
    </div>
  `
  setTimeout(() => {
    drawSparkline('sparkline-container-cpu', cpuHistory, 100, '#6366f1')
    drawSparkline('sparkline-container-mem', memHistory, 100, '#10b981')
  }, 50)
}

export async function openServerConfig() {
  if (!state.selectedServer) return
  try {
    const s = await fetch(`/api/servers/${state.selectedServer.id}`).then(r => r.json())
    document.getElementById('cfg-maxplayers').value = s.max_players || ''
    document.getElementById('cfg-port').value       = s.port        || ''
    document.getElementById('cfg-password').value   = ''
    document.getElementById('cfg-rcon').value       = ''
    document.getElementById('cfg-status').textContent = ''

    const gameDef  = state.gameDefs.find(d => d.slug === state.selectedServer.game_slug)
    const fields   = parseJSON(gameDef?.custom_fields, [])
    const cfgEl    = document.getElementById('cfg-custom-fields')
    cfgEl.innerHTML = fields.map(f => `
      <div class="form-field">
        <label class="form-label">${escapeHtml(f.label)}</label>
        <input class="form-input" id="cfg-cf-${escapeHtml(f.key)}"
               placeholder="${escapeHtml(f.placeholder || '')}"
               type="${f.type === 'password' ? 'password' : 'text'}"
               value="${escapeHtml(s.custom_config?.[f.key] || '')}" />
      </div>
    `).join('')
  } catch(e) {
    console.error('openServerConfig error', e)
  }
}

export async function saveServerConfig() {
  if (!state.selectedServer) return
  const gameDef = state.gameDefs.find(d => d.slug === state.selectedServer.game_slug)
  const fields  = parseJSON(gameDef?.custom_fields, [])
  const customConfig = {}
  fields.forEach(f => {
    const el = document.getElementById(`cfg-cf-${f.key}`)
    if (el) customConfig[f.key] = el.value.trim()
  })
  const body = {
    port:          parseInt(document.getElementById('cfg-port').value)       || 0,
    max_players:   parseInt(document.getElementById('cfg-maxplayers').value) || 0,
    password:      document.getElementById('cfg-password').value.trim(),
    rcon_password: document.getElementById('cfg-rcon').value.trim(),
    custom_config: customConfig,
  }
  const res = await fetch(`/api/servers/${state.selectedServer.id}`, {
    method:  'PUT',
    headers: { 'Content-Type': 'application/json' },
    body:    JSON.stringify(body),
  })
  if (res.ok) {
    document.getElementById('cfg-status').textContent = 'Saved!'
    setTimeout(() => {
      const el = document.getElementById('cfg-status')
      if (el) el.textContent = ''
    }, 2000)
    window.loadData && window.loadData()
  } else {
    const data = await res.json().catch(() => ({}))
    document.getElementById('cfg-status').textContent = 'Error: ' + (data.error || 'save failed')
  }
}

export async function saveAndRestart() {
  if (!state.selectedServer) return
  await saveServerConfig()
  const status = state.selectedServer.status
  if (status === 'running' || status === 'starting') {
    state.pendingRestart = state.selectedServer.id
    await fetch(`/api/servers/${state.selectedServer.id}/stop`, { method: 'POST' })
  } else {
    await fetch(`/api/servers/${state.selectedServer.id}/start`, { method: 'POST' })
  }
  window.loadData && window.loadData()
}

export async function startServer() {
  if (!state.selectedServer) return
  document.getElementById('btn-start').disabled = true
  try {
    await fetch(`/api/servers/${state.selectedServer.id}/start`, { method: 'POST' })
  } catch(e) { console.error('start error', e) }
  window.loadData && window.loadData()
}

export async function stopServer() {
  if (!state.selectedServer) return
  document.getElementById('btn-stop').disabled = true
  try {
    await fetch(`/api/servers/${state.selectedServer.id}/stop`, { method: 'POST' })
  } catch(e) { console.error('stop error', e) }
  window.loadData && window.loadData()
}

export async function deleteServer() {
  if (!state.selectedServer) return
  if (!await window.showConfirm(
    `Delete "${state.selectedServer.name}"? This will remove the container and all game data. This cannot be undone.`,
    'Delete Server'
  )) return
  try {
    await fetch(`/api/servers/${state.selectedServer.id}`, { method: 'DELETE' })
    closeServer()
    window.loadData && window.loadData()
  } catch(e) {
    window.showToast('Failed to delete server', 'error')
  }
}
