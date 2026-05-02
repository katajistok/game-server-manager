import { state } from './state.js'
import { formatMB, drawSparkline } from './utils.js'
import { wsSend } from './ws.js'

export function openAgent(id) {
  state.selectedAgent = state.agents.find(a => a.id === id)
  state.selectedServer = null
  if (!state.selectedAgent) return

  showPanel('agent-panel')

  document.getElementById('agent-panel-name').textContent = state.selectedAgent.name
  const info = state.selectedAgent.host_info || {}
  document.getElementById('agent-info-os').textContent    = info.os        || '—'
  document.getElementById('agent-info-cores').textContent = info.cpu_cores || '—'
  document.getElementById('agent-disk-val').textContent   = info.disk_gb   ? info.disk_gb + ' GB' : '—'

  fetch(`/api/metrics/${encodeURIComponent(state.selectedAgent.name)}`)
    .then(r => r.json())
    .then(history => {
      if (history && history.length) {
        state.agentMetricsHistory[state.selectedAgent.name] = history
        renderAgentMetrics(state.selectedAgent.name)
      }
    })
    .catch(e => console.error('metrics load error', e))
}

export function renderAgentMetrics(agentName) {
  const history = state.agentMetricsHistory[agentName] || []
  if (!history.length) return
  const latest = history[history.length - 1]
  document.getElementById('agent-cpu-val').textContent =
    latest.cpu_percent.toFixed(1) + '%'
  document.getElementById('agent-mem-val').textContent =
    formatMB(latest.mem_used_mb) + ' / ' + formatMB(latest.mem_total_mb)
  document.getElementById('agent-disk-val').textContent =
    latest.disk_used_gb + ' GB / ' + latest.disk_total_gb + ' GB'
  drawSparkline('sparkline-cpu', history.map(h => h.cpu_percent), 100, '#6366f1')
  drawSparkline('sparkline-mem', history.map(h => (h.mem_used_mb / h.mem_total_mb) * 100), 100, '#10b981')
}

export function requestMetricsRefresh() {
  const btn = document.getElementById('btn-refresh-metrics')
  if (btn) {
    btn.disabled = true
    btn.textContent = '↻ Refreshing...'
    setTimeout(() => { btn.disabled = false; btn.textContent = '↻ Refresh' }, 3000)
  }
  wsSend({ type: 'request_metrics' })
}

export function showPanel(panelId) {
  ['empty-state', 'server-panel', 'admin-panel', 'agent-panel'].forEach(id => {
    const el = document.getElementById(id)
    if (el) el.style.display = id === panelId ? 'flex' : 'none'
  })
}
