export function escapeHtml(str) {
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

export function formatMB(mb) {
  if (mb >= 1024) return (mb / 1024).toFixed(1) + ' GB'
  return mb + ' MB'
}

export function parseJSON(val, fallback) {
  if (val == null) return fallback
  try { return typeof val === 'string' ? JSON.parse(val) : val }
  catch(e) { return fallback }
}

export function drawSparkline(canvasId, data, maxVal, color) {
  const canvas = document.getElementById(canvasId)
  if (!canvas || !data.length) return
  const ctx = canvas.getContext('2d')
  const w = canvas.width
  const h = canvas.height
  ctx.clearRect(0, 0, w, h)
  ctx.fillStyle = '#0c0e16'
  ctx.fillRect(0, 0, w, h)
  if (data.length < 2) return
  const step = w / (data.length - 1)
  ctx.beginPath()
  ctx.moveTo(0, h)
  data.forEach((val, i) => ctx.lineTo(i * step, h - (Math.min(val, maxVal) / maxVal) * h))
  ctx.lineTo(w, h)
  ctx.closePath()
  ctx.fillStyle = color + '22'
  ctx.fill()
  ctx.beginPath()
  data.forEach((val, i) => {
    const x = i * step
    const y = h - (Math.min(val, maxVal) / maxVal) * h
    if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y)
  })
  ctx.strokeStyle = color
  ctx.lineWidth = 2
  ctx.stroke()
}
