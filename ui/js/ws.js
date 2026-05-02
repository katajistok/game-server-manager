import { state } from './state.js'

export function connectWebSocket(onMessage) {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  const ws = new WebSocket(`${proto}://${location.host}/ws/ui`)
  state.ws = ws

  ws.onopen  = () => { state.wsConnected = true;  setWsStatus(true) }
  ws.onclose = () => { state.wsConnected = false; setWsStatus(false); setTimeout(() => connectWebSocket(onMessage), 3000) }
  ws.onerror = () => ws.close()
  ws.onmessage = (event) => {
    try { onMessage(JSON.parse(event.data)) }
    catch(e) { console.error('ws parse error', e) }
  }
}

export function wsSend(msg) {
  if (state.ws && state.ws.readyState === WebSocket.OPEN)
    state.ws.send(JSON.stringify(msg))
}

export function setWsStatus(connected) {
  document.getElementById('ws-dot').className = `ws-dot ${connected ? 'connected' : ''}`
  document.getElementById('ws-label').textContent = connected ? 'Connected' : 'Reconnecting...'
}
