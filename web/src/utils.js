export function escapeHtml(s) {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

const URL_RE = /(https?:\/\/[^\s<>"']+)/g

// 先整体转义再做链接替换，防 XSS；长 URL 靠 CSS overflow-wrap 换行
export function linkify(text) {
  return escapeHtml(text).replace(URL_RE, (m) => {
    return `<a href="${m}" target="_blank" rel="noopener noreferrer">${m}</a>`
  })
}

export function formatSize(bytes) {
  if (!bytes || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let i = 0
  let n = bytes
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return (i === 0 || n >= 100 ? n.toFixed(0) : n.toFixed(1)) + ' ' + units[i]
}

const pad = (x) => String(x).padStart(2, '0')

export function formatTime(ms) {
  const d = new Date(ms)
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function formatDay(ms) {
  const d = new Date(ms)
  const now = new Date()
  const start = (x) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime()
  const diffDays = Math.round((start(now) - start(d)) / 86400000)
  if (diffDays === 0) return '今天'
  if (diffDays === 1) return '昨天'
  return `${d.getFullYear()} 年 ${d.getMonth() + 1} 月 ${d.getDate()} 日`
}
