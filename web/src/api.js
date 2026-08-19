async function request(path, opts = {}) {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json', ...(opts.headers || {}) },
    ...opts,
  })
  let data = {}
  try {
    data = await res.json()
  } catch {
    /* 非 JSON 响应 */
  }
  if (res.status === 401) {
    const err = new Error(data.error || '未登录')
    err.status = 401
    throw err
  }
  if (!res.ok) {
    throw new Error(data.error || `HTTP ${res.status}`)
  }
  return data
}

export const api = {
  login: (password) => request('/api/login', { method: 'POST', body: JSON.stringify({ password }) }),
  logout: () => request('/api/logout', { method: 'POST' }),
  stats: () => request('/api/stats'),
  messages: (params) => request('/api/messages?' + new URLSearchParams(params)),
  sendText: (content) => request('/api/messages', { method: 'POST', body: JSON.stringify({ content }) }),
  deleteMessage: (id) => request(`/api/messages/${id}`, { method: 'DELETE' }),
  cleanup: (days) => request('/api/cleanup', { method: 'POST', body: JSON.stringify({ days }) }),
  clearAll: () => request('/api/clear', { method: 'POST' }),
  search: (q) => request('/api/search?q=' + encodeURIComponent(q)),
  backupNow: () => request('/api/backup', { method: 'POST' }),
  fileUrl: (fileId) => '/api/files/' + encodeURIComponent(fileId),
  downloadUrl: (fileId) => '/api/files/' + encodeURIComponent(fileId) + '?download=1',
  uploadFile(file, onProgress) {
    return new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest()
      xhr.open('POST', '/api/upload')
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable && onProgress) onProgress(e.loaded / e.total)
      }
      xhr.onload = () => {
        let data = {}
        try {
          data = JSON.parse(xhr.responseText)
        } catch {
          /* ignore */
        }
        if (xhr.status === 401) {
          const err = new Error('未登录')
          err.status = 401
          reject(err)
          return
        }
        if (xhr.status >= 200 && xhr.status < 300) {
          resolve(data)
        } else {
          reject(new Error(data.error || '上传失败'))
        }
      }
      xhr.onerror = () => reject(new Error('网络错误'))
      const fd = new FormData()
      fd.append('file', file)
      xhr.send(fd)
    })
  },
}
