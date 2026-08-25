async function request(path, opts = {}) {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json', ...(opts.headers || {}) },
    ...opts,
  })
  let data = {}
  try { data = await res.json() } catch { /* 非 JSON */ }
  if (!res.ok) {
    throw new Error(data.error || `HTTP ${res.status}`)
  }
  return data
}

export const api = {
  // 房间
  listRooms: () => request('/api/rooms'),
  createRoom: (name, password) => request('/api/rooms', { method: 'POST', body: JSON.stringify({ name, password }) }),
  loginRoom: (room, password) => request(`/api/rooms/${encodeURIComponent(room)}/login`, { method: 'POST', body: JSON.stringify({ password }) }),
  logoutRoom: () => request('/api/rooms/logout', { method: 'POST' }),

  // 消息
  messages: (params) => request('/api/messages?' + new URLSearchParams(params)),
  sendText: (content) => request('/api/messages', { method: 'POST', body: JSON.stringify({ content }) }),
  deleteMessage: (id) => request(`/api/messages/${id}`, { method: 'DELETE' }),

  // 回收站
  trash: () => request('/api/trash'),
  restore: (id) => request(`/api/trash/${id}/restore`, { method: 'POST' }),
  permanentDelete: (id) => request(`/api/trash/${id}`, { method: 'DELETE' }),
  emptyTrash: () => request('/api/trash/empty', { method: 'POST' }),

  // 其他
  cleanup: (days) => request('/api/cleanup', { method: 'POST', body: JSON.stringify({ days }) }),
  clearAll: () => request('/api/clear', { method: 'POST' }),
  search: (q) => request('/api/search?q=' + encodeURIComponent(q)),
  stats: () => request('/api/stats'),
  backupNow: () => request('/api/backup', { method: 'POST' }),

  fileUrl: (fileId) => '/api/files/' + encodeURIComponent(fileId),
  downloadUrl: (fileId) => '/api/files/' + encodeURIComponent(fileId) + '?download=1',

  uploadFile(file, onProgress) {
    return new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest()
      xhr.open('POST', '/api/upload')
      xhr.upload.onprogress = (e) => { if (e.lengthComputable && onProgress) onProgress(e.loaded / e.total) }
      xhr.onload = () => {
        let data = {}
        try { data = JSON.parse(xhr.responseText) } catch { /* ignore */ }
        if (xhr.status >= 200 && xhr.status < 300) resolve(data)
        else reject(new Error(data.error || '上传失败'))
      }
      xhr.onerror = () => reject(new Error('网络错误'))
      const fd = new FormData()
      fd.append('file', file)
      xhr.send(fd)
    })
  },
}
