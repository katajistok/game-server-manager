import { state } from './state.js'
import { escapeHtml } from './utils.js'

export function renderSidebar() {
  renderAgents()
  renderServers()
}

export function renderAgents() {
  const el = document.getElementById('agents-list')
  if (!state.agents.length) {
    el.innerHTML = '<div class="sidebar-empty">No agents registered</div>'
    return
  }
  el.innerHTML = state.agents.map(a => `
    <div class="agent-row ${state.selectedAgent?.id === a.id ? 'agent-row-active' : ''}"
         onclick="openAgent(${a.id})">
      <span>${a.connected ? '🟢' : '⚫'} ${escapeHtml(a.name)}</span>
      <span class="badge badge-${a.status}">${a.status}</span>
    </div>
  `).join('')
}

export function renderServers() {
  const el = document.getElementById('servers-list')
  if (!state.servers.length) {
    el.innerHTML = '<div class="sidebar-empty">No servers</div>'
    return
  }
  el.innerHTML = state.servers.map(s => `
    <div class="server-row ${state.selectedServer?.id === s.id ? 'active' : ''}"
         onclick="openServer(${s.id})">
      <div class="server-row-top">
        <span class="server-name">${escapeHtml(s.name)}</span>
        <span class="badge badge-${s.status}">${s.status}</span>
      </div>
      <div class="server-meta">
        ${escapeHtml((s.game_slug || '').toUpperCase())}
        · ${escapeHtml(s.mode_slug || '')}
        · :${s.port}
        ${s.has_password ? ' · 🔒' : ''}
      </div>
    </div>
  `).join('')
}
