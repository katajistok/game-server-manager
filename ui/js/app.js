import './games/cs2.js'

import { state } from './state.js'
import { connectWebSocket, wsSend } from './ws.js'
import { renderSidebar, renderAgents, renderServers } from './sidebar.js'
import { openAgent, renderAgentMetrics, requestMetricsRefresh, showPanel } from './agent.js'
import {
  openServer, closeServer, updateServerHeader, switchTab,
  appendLog, appendRcon, handleRconKey, sendRcon,
  renderContainerMetrics, openServerConfig, saveServerConfig, saveAndRestart,
  startServer, stopServer, deleteServer,
} from './server.js'
import {
  loadAdminData, updateModeSelect, updateFormDefaults,
  approveAgent, deleteAgent, createServer,
} from './admin.js'

async function loadData() {
  try {
    const [servers, agents] = await Promise.all([
      fetch('/api/servers').then(r => r.json()),
      fetch('/api/agents').then(r => r.json()),
    ])
    state.servers = servers
    state.agents  = agents

    if (!state.gameDefs.length) {
      state.gameDefs = await fetch('/api/game-definitions').then(r => r.json())
    }

    renderSidebar()

    if (state.selectedServer) {
      const updated = state.servers.find(s => s.id === state.selectedServer.id)
      if (updated) updateServerHeader(updated)
    }

    document.getElementById('empty-subtitle').textContent =
      `${state.servers.length} server${state.servers.length !== 1 ? 's' : ''} registered`
  } catch(e) {
    console.error('loadData error', e)
  }
}

function switchMainView(view) {
  ['empty-state', 'server-panel', 'admin-panel', 'agent-panel'].forEach(id => {
    document.getElementById(id).style.display = 'none'
  })
  if (view === 'admin') {
    document.getElementById('admin-panel').style.display = 'flex'
    loadAdminData()
    state.selectedServer = null
    state.selectedAgent  = null
    renderAgents()
    renderServers()
  } else {
    document.getElementById('empty-state').style.display = 'flex'
  }
}

function handleWsMessage(msg) {
  switch(msg.type) {
    case 'server_status': {
      const idx = state.servers.findIndex(s => s.id === msg.payload.server_id)
      if (idx !== -1) {
        state.servers[idx].status = msg.payload.status
        if (msg.payload.container_id) state.servers[idx].container_id = msg.payload.container_id
        renderServers()
        if (state.selectedServer?.id === msg.payload.server_id) updateServerHeader(state.servers[idx])
        if (msg.payload.status === 'stopped' && state.pendingRestart === msg.payload.server_id) {
          state.pendingRestart = null
          fetch(`/api/servers/${msg.payload.server_id}/start`, { method: 'POST' }).then(() => loadData())
        }
      }
      break
    }
    case 'log_line':
      if (state.selectedServer?.id === msg.payload.server_id)
        appendLog(msg.payload.line, msg.payload.time)
      break

    case 'rcon_response':
      if (state.selectedServer?.id === msg.payload.server_id) {
        if (msg.payload.error) appendRcon('error',    msg.payload.error)
        else                   appendRcon('response', msg.payload.response)
      }
      break

    case 'metrics':
      handleMetrics(msg.payload)
      break
  }
}

function handleMetrics(payload) {
  if (!state.agentMetricsHistory[payload.agent_name]) state.agentMetricsHistory[payload.agent_name] = []
  const history = state.agentMetricsHistory[payload.agent_name]
  history.push(payload)
  if (history.length > 30) history.splice(0, history.length - 30)

  if (payload.containers) {
    payload.containers.forEach(c => {
      if (!state.containerMetricsHistory[c.container_name]) state.containerMetricsHistory[c.container_name] = []
      const ch = state.containerMetricsHistory[c.container_name]
      ch.push(c)
      if (ch.length > 30) ch.splice(0, ch.length - 30)
    })
  }

  if (state.selectedAgent?.name === payload.agent_name) renderAgentMetrics(payload.agent_name)

  if (state.selectedServer) {
    const activeTab = document.querySelector('.tab-btn.active')
    if (activeTab && activeTab.textContent === 'Metrics')
      renderContainerMetrics(state.selectedServer.container_name)
  }
}

function showToast(message, type = 'info') {
  const container = document.getElementById('toast-container')
  const el = document.createElement('div')
  el.className = `toast${type === 'success' ? ' toast-success' : type === 'error' ? ' toast-error' : ''}`
  el.textContent = message
  container.appendChild(el)
  setTimeout(() => {
    el.classList.add('toast-out')
    el.addEventListener('animationend', () => el.remove())
  }, 3500)
}

function showConfirm(message, title = 'Are you sure?') {
  return new Promise(resolve => {
    document.getElementById('confirm-title').textContent   = title
    document.getElementById('confirm-message').textContent = message
    const modal  = document.getElementById('confirm-modal')
    const okBtn  = document.getElementById('confirm-ok')
    const cancelBtn = document.getElementById('confirm-cancel')
    modal.style.display = 'flex'
    const finish = (result) => {
      modal.style.display = 'none'
      okBtn.removeEventListener('click', onOk)
      cancelBtn.removeEventListener('click', onCancel)
      resolve(result)
    }
    const onOk     = () => finish(true)
    const onCancel = () => finish(false)
    okBtn.addEventListener('click', onOk)
    cancelBtn.addEventListener('click', onCancel)
  })
}

function copyAgentToken() {
  const input = document.getElementById('token-modal-input')
  navigator.clipboard.writeText(input.value).then(() => {
    document.getElementById('token-modal-copied').textContent = 'Copied!'
  })
}

function closeTokenModal() {
  document.getElementById('token-modal').style.display = 'none'
}

// Expose functions needed by inline onclick handlers
window.openAgent          = openAgent
window.openServer         = openServer
window.closeServer        = closeServer
window.switchTab          = switchTab
window.startServer        = startServer
window.stopServer         = stopServer
window.deleteServer       = deleteServer
window.handleRconKey      = handleRconKey
window.sendRcon           = sendRcon
window.saveServerConfig   = saveServerConfig
window.saveAndRestart     = saveAndRestart
window.requestMetricsRefresh = requestMetricsRefresh
window.switchMainView     = switchMainView
window.approveAgent       = approveAgent
window.deleteAgent        = deleteAgent
window.createServer       = createServer
window.updateModeSelect   = updateModeSelect
window.updateFormDefaults = updateFormDefaults
window.loadData           = loadData
window.copyAgentToken     = copyAgentToken
window.closeTokenModal    = closeTokenModal
window.showToast          = showToast
window.showConfirm        = showConfirm

document.addEventListener('DOMContentLoaded', () => {
  connectWebSocket(handleWsMessage)
  loadData()
  setInterval(loadData, 5000)
})
