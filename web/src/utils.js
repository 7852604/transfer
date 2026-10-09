export function escapeHtml(s) {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

const URL_RE = /(https?:\/\/[^\s<>"']+)/g

function escapeRegExp(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

// 对已转义的文本段做关键词高亮（多关键词、不区分大小写）
function highlightSegments(html, highlight) {
  const words = highlight.trim().split(/\s+/).filter(Boolean)
  if (!words.length) return html
  const re = new RegExp(words.map((w) => escapeRegExp(escapeHtml(w))).join('|'), 'gi')
  // 只处理非标签文本段：<a ...>...</a> 保持原样，纯文本段（不含 <）才高亮
  return html.replace(/(<a [^>]*>[\s\S]*?<\/a>)|([^<]+)/g, (seg, anchor, text) => {
    if (anchor) return anchor
    return text.replace(re, (m) => `<mark class="search-hit">${m}</mark>`)
  })
}

// 先整体转义再做链接替换，防 XSS；long URL 靠 CSS overflow-wrap 换行。
// highlight 为搜索关键词，命中处包 <mark> 高亮。
export function linkify(text, highlight = '') {
  const html = escapeHtml(text).replace(URL_RE, (m) => {
    return `<a href="${m}" target="_blank" rel="noopener noreferrer">${m}</a>`
  })
  return highlight ? highlightSegments(html, highlight) : html
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
