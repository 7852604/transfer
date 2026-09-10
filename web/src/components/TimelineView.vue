<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api } from '../api'
import { formatDay, formatSize } from '../utils'
import MessageItem from './MessageItem.vue'
import ImageViewer from './ImageViewer.vue'

const messages = ref([])
const hasMore = ref(false)
const loadingOlder = ref(false)
const searchQuery = ref('')
const searchResults = ref(null)
const stats = ref(null)
const draft = ref('')
const uploads = ref([])
const toast = ref('')
const showSidebar = ref(false)
const showRoomDialog = ref(false)
const showTrash = ref(false)
const showCleanup = ref(false)
const showClear = ref(false)
const showPasswordDialog = ref(false)
const passwordInput = ref('')
const passwordError = ref('')
const showDeleteRoom = ref(false)
const adminPassword = ref('')
const deleteRoomError = ref('')
const deleting = ref(false)
const viewerMsg = ref(null)
const dragging = ref(false)

// 暗色模式：'auto' | 'light' | 'dark'
const theme = ref(localStorage.getItem('theme') || 'auto')

// 房间状态
const rooms = ref([])
const currentRoom = ref(null)
const roomPassword = ref('')
const roomLoginForm = ref(null) // {name, error}
const newRoomName = ref('')
const newRoomPassword = ref('')

// 回收站
const trashItems = ref([])

const listEl = ref(null)
const inputEl = ref(null)
const fileInputEl = ref(null)

const isSearching = computed(() => searchResults.value !== null)
const displayList = computed(() => (isSearching.value ? searchResults.value : messages.value))

const groups = computed(() => {
  const gs = []
  let lastDay = ''
  for (const m of displayList.value) {
    const day = formatDay(m.createdAt)
    if (day !== lastDay) { gs.push({ day, items: [m] }); lastDay = day }
    else { gs[gs.length - 1].items.push(m) }
  }
  return gs
})

const POLL_MS = 5000
let pollTimer = null
let searchTimer = null
let toastTimer = null
let uid = 0
let dragDepth = 0

onMounted(async () => {
  applyTheme(theme.value)
  // 系统主题变化时，若处于跟随模式则实时切换
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    if (theme.value === 'auto') applyTheme('auto')
  })
  await loadRooms()
  await loadInitial()
  pollTimer = setInterval(poll, POLL_MS)
  document.addEventListener('paste', onPaste)
  document.addEventListener('visibilitychange', onVisible)
})
onBeforeUnmount(() => {
  clearInterval(pollTimer)
  clearTimeout(searchTimer)
  document.removeEventListener('paste', onPaste)
  document.removeEventListener('visibilitychange', onVisible)
})

// ---------- 房间 ----------

async function loadRooms() {
  try {
    const data = await api.listRooms()
    rooms.value = data.rooms
    // 默认选 BUG反馈（ID=1）
    if (!currentRoom.value) {
      currentRoom.value = data.rooms.find(r => r.id === 1) || data.rooms[0]
    }
  } catch (e) { showToast('读取房间失败') }
}

async function switchRoom(room) {
  showSidebar.value = false
  if (room.hasPassword && room.id !== currentRoom.value?.id) {
    roomLoginForm.value = { name: room.name, error: '' }
    return
  }
  // 切换：调 login 接口设置 cookie
  try {
    await api.loginRoom(room.name, '')
    currentRoom.value = room
    messages.value = []
    await loadInitial()
  } catch (e) { showToast('切换房间失败') }
}

async function submitRoomLogin() {
  if (!roomLoginForm.value) return
  try {
    const data = await api.loginRoom(roomLoginForm.value.name, roomPassword.value)
    currentRoom.value = data.room
    roomLoginForm.value = null
    roomPassword.value = ''
    messages.value = []
    await loadInitial()
  } catch (e) {
    roomLoginForm.value.error = e.message
  }
}

async function createRoom() {
  const name = newRoomName.value.trim()
  if (!name) return
  try {
    const room = await api.createRoom(name, newRoomPassword.value.trim())
    rooms.value = [...rooms.value, room]
    currentRoom.value = room
    newRoomName.value = ''
    newRoomPassword.value = ''
    showRoomDialog.value = false
    showSidebar.value = false
    messages.value = []
    await loadInitial()
  } catch (e) { showToast(e.message) }
}

// 给当前房间设置/修改/取消密码
async function submitSetPassword() {
  passwordError.value = ''
  const pw = passwordInput.value.trim()
  try {
    const updated = await api.setRoomPassword(pw)
    // 更新 rooms 列表里对应项
    rooms.value = rooms.value.map((r) => (r.id === updated.id ? updated : r))
    currentRoom.value = updated
    showPasswordDialog.value = false
    passwordInput.value = ''
    showToast(updated.hasPassword ? '已设置密码' : '已取消加密')
  } catch (e) { passwordError.value = e.message || '设置失败' }
}

// ---------- 置顶 ----------

const pinnedMsg = computed(() => {
  const pid = currentRoom.value?.pinnedMsgId
  if (!pid) return null
  return messages.value.find((m) => m.id === pid) || null
})

async function togglePin(msg) {
  const target = pinnedMsg.value?.id === msg.id ? 0 : msg.id
  try {
    const updated = await api.pinMessage(target)
    rooms.value = rooms.value.map((r) => (r.id === updated.id ? updated : r))
    currentRoom.value = updated
    showToast(target ? '已置顶' : '已取消置顶')
  } catch (e) { showToast(e.message || '置顶失败') }
}

// ---------- 删除房间 ----------

async function submitDeleteRoom() {
  if (deleting.value) return
  deleteRoomError.value = ''
  deleting.value = true
  try {
    await api.deleteRoom(adminPassword.value)
    showDeleteRoom.value = false
    adminPassword.value = ''
    rooms.value = rooms.value.filter((r) => r.id !== currentRoom.value?.id)
    currentRoom.value = rooms.value.find((r) => r.id === 1) || rooms.value[0]
    messages.value = []
    await loadInitial()
    showToast('房间已删除')
  } catch (e) {
    deleteRoomError.value = e.message || '删除失败'
  }
  deleting.value = false
}

// ---------- 暗色模式 ----------

function applyTheme(t) {
  const dark = t === 'dark' || (t === 'auto' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.dataset.theme = dark ? 'dark' : 'light'
}

function cycleTheme() {
  const order = ['auto', 'light', 'dark']
  theme.value = order[(order.indexOf(theme.value) + 1) % order.length]
  localStorage.setItem('theme', theme.value)
  applyTheme(theme.value)
  showToast(theme.value === 'auto' ? '跟随系统' : theme.value === 'dark' ? '暗色模式' : '亮色模式')
}

const themeIcon = computed(() => (theme.value === 'auto' ? '🌗' : theme.value === 'dark' ? '🌙' : '☀️'))
const themeLabel = computed(() => (theme.value === 'auto' ? '主题：跟随系统' : theme.value === 'dark' ? '主题：暗色' : '主题：亮色'))

const appVersion = typeof __APP_VERSION__ !== 'undefined' ? __APP_VERSION__ : 'dev'
const buildTime = typeof __BUILD_TIME__ !== 'undefined' ? __BUILD_TIME__ : ''

function showVersion() {
  showToast(`速传 v${appVersion}${buildTime ? ' · 构建 ' + buildTime : ''}`)
}

// ---------- 消息加载 ----------

async function loadInitial() {
  try {
    const data = await api.messages({ limit: 200 })
    messages.value = data.messages
    hasMore.value = data.hasMore
    scrollBottom()
    refreshStats()
  } catch (e) { showToast('加载失败：' + e.message) }
}

async function poll() {
  if (document.hidden || isSearching.value || showTrash.value) return
  const lastId = messages.value.length ? messages.value[messages.value.length - 1].id : 0
  try {
    const data = await api.messages({ after: lastId, limit: 100 })
    if (data.messages.length) {
      const nearBottom = isNearBottom()
      messages.value.push(...data.messages)
      if (nearBottom) scrollBottom()
      refreshStats()
    }
  } catch (e) { /* ignore */ }
}

function onVisible() { if (!document.hidden) { poll(); refreshStats() } }

function isNearBottom() {
  const el = listEl.value; if (!el) return true
  return el.scrollHeight - el.scrollTop - el.clientHeight < 120
}
function scrollBottom() { nextTick(() => { const el = listEl.value; if (el) el.scrollTop = el.scrollHeight }) }
function onScroll() {
  const el = listEl.value
  if (!el || loadingOlder.value || !hasMore.value || isSearching.value) return
  if (el.scrollTop < 60) loadMore()
}
async function loadMore() {
  if (!messages.value.length) return
  loadingOlder.value = true
  const el = listEl.value
  const prevHeight = el ? el.scrollHeight : 0
  try {
    const data = await api.messages({ before: messages.value[0].id, limit: 200 })
    messages.value = [...data.messages, ...messages.value]
    hasMore.value = data.hasMore
    nextTick(() => { if (el) el.scrollTop = el.scrollHeight - prevHeight })
  } catch (e) { showToast('加载失败') }
  loadingOlder.value = false
}

// ---------- 发送 ----------

function appendMessage(msg) {
  const last = messages.value[messages.value.length - 1]
  if (!last || msg.id > last.id) { messages.value.push(msg); scrollBottom() }
}

async function sendText() {
  const content = draft.value.trim()
  if (!content) return
  draft.value = ''
  nextTick(() => autoGrow())
  try {
    const msg = await api.sendText(content)
    appendMessage(msg)
    refreshStats()
  } catch (e) { draft.value = content; showToast('发送失败：' + e.message) }
}

function onEnterKey(e) { if (e.isComposing || e.keyCode === 229) return; sendText() }

function autoGrow() {
  const el = inputEl.value; if (!el) return
  el.style.height = 'auto'
  el.style.height = Math.min(el.scrollHeight, 140) + 'px'
}

// ---------- 上传 ----------

async function handleFiles(files) {
  const list = Array.from(files || [])
  if (!list.length) return
  for (const f of list) {
    const item = { key: ++uid, name: f.name, size: f.size, progress: 0, error: '' }
    uploads.value.push(item)
    try {
      const data = await api.uploadFile(f, (p) => (item.progress = p))
      const msgs = data.messages || []
      if (msgs[0]) { appendMessage(msgs[0]); refreshStats() }
      uploads.value = uploads.value.filter((u) => u.key !== item.key)
    } catch (e) { item.error = e.message || '上传失败' }
  }
}
function dismissUpload(key) { uploads.value = uploads.value.filter((u) => u.key !== key) }
function onFilePick(e) { handleFiles(e.target.files); e.target.value = '' }
function onPaste(e) { const files = e.clipboardData?.files; if (files?.length) { e.preventDefault(); handleFiles(files) } }
function onDragEnter(e) { e.preventDefault(); dragDepth++; dragging.value = true }
function onDragLeave() { dragDepth--; if (dragDepth <= 0) { dragging.value = false; dragDepth = 0 } }
function onDrop(e) { dragging.value = false; dragDepth = 0; handleFiles(e.dataTransfer?.files) }

// ---------- 搜索 ----------

watch(searchQuery, (q) => {
  clearTimeout(searchTimer)
  const query = q.trim()
  if (!query) { searchResults.value = null; return }
  searchTimer = setTimeout(async () => {
    try { searchResults.value = (await api.search(query)).messages || [] } catch { /* ignore */ }
  }, 300)
})

// ---------- 消息操作 ----------

async function deleteMessage(msg) {
  try {
    await api.deleteMessage(msg.id)
    messages.value = messages.value.filter((m) => m.id !== msg.id)
    if (isSearching.value) searchResults.value = searchResults.value.filter((m) => m.id !== msg.id)
    refreshStats()
  } catch (e) { showToast('删除失败') }
}

// ---------- 回收站 ----------

async function openTrash() {
  showSidebar.value = false
  showTrash.value = true
  try {
    const data = await api.trash()
    trashItems.value = data.messages || []
  } catch (e) { showToast('读取回收站失败') }
}

async function restoreMessage(msg) {
  try {
    await api.restore(msg.id)
    trashItems.value = trashItems.value.filter((m) => m.id !== msg.id)
    messages.value.push(msg)
    messages.value.sort((a, b) => a.id - b.id)
    refreshStats()
    showToast('已恢复')
  } catch (e) { showToast('恢复失败') }
}

async function permanentDelete(msg) {
  try {
    await api.permanentDelete(msg.id)
    trashItems.value = trashItems.value.filter((m) => m.id !== msg.id)
    refreshStats()
  } catch (e) { showToast('删除失败') }
}

async function emptyTrash() {
  try {
    const r = await api.emptyTrash()
    trashItems.value = []
    showToast(`已清空回收站，释放 ${formatSize(r.freedBytes)}`)
    refreshStats()
  } catch (e) { showToast('清空失败') }
}

// ---------- 菜单操作 ----------

async function backupNow() {
  showSidebar.value = false
  showToast('备份中…')
  try {
    const r = await api.backupNow()
    showToast(`备份完成：${formatSize(r.size)}`)
    refreshStats()
  } catch (e) { showToast('备份失败') }
}

async function doCleanup(days) {
  showCleanup.value = false
  try {
    const r = await api.cleanup(days)
    showToast(`已删除 ${r.deleted} 条，释放 ${formatSize(r.freedBytes)}`)
    await loadInitial()
  } catch (e) { showToast('清理失败') }
}

async function doClear() {
  showClear.value = false
  try {
    await api.clearAll()
    messages.value = []
    hasMore.value = false
    refreshStats()
    showToast('已清空')
  } catch (e) { showToast('清空失败') }
}

async function refreshStats() {
  try { stats.value = await api.stats() } catch { /* ignore */ }
}

function showToast(msg) {
  toast.value = msg
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => (toast.value = ''), 2600)
}
</script>

<template>
  <div class="shell" @dragenter="onDragEnter" @dragover.prevent @dragleave="onDragLeave" @drop.prevent="onDrop">
    <!-- 侧边栏遮罩（移动端） -->
    <div v-if="showSidebar" class="sidebar-backdrop" @click="showSidebar = false"></div>

    <!-- 侧边栏 -->
    <aside class="sidebar" :class="{ open: showSidebar }">
      <div class="sidebar-head">
        <span class="sidebar-title">📨 速传</span>
        <button class="sidebar-new-btn" title="新建房间" @click="showRoomDialog = true; showSidebar = false">＋</button>
      </div>

      <nav class="sidebar-rooms">
        <div class="sidebar-section">房间</div>
        <button v-for="r in rooms" :key="r.id" class="sidebar-room" :class="{ active: r.id === currentRoom?.id }" @click="switchRoom(r)">
          <span v-if="r.hasPassword" class="room-lock">🔒</span>
          <span v-else class="room-lock muted">🔓</span>
          <span class="room-name">{{ r.name }}</span>
          <span v-if="r.id === currentRoom?.id" class="room-check">✓</span>
        </button>
      </nav>

      <div class="sidebar-tools">
        <div class="sidebar-section">工具</div>
        <button class="sidebar-tool" @click="showPasswordDialog = true; showSidebar = false">
          <span>🔐</span><span>{{ currentRoom?.hasPassword ? '修改 / 取消密码' : '设置密码' }}</span>
        </button>
        <button class="sidebar-tool" @click="openTrash"><span>🗑</span><span>回收站</span></button>
        <button class="sidebar-tool" @click="backupNow"><span>💾</span><span>立即备份</span></button>
        <button class="sidebar-tool" @click="showCleanup = true; showSidebar = false"><span>🧹</span><span>清理旧消息</span></button>
        <button class="sidebar-tool" @click="showClear = true; showSidebar = false"><span>⚠</span><span>清空本房间</span></button>
        <button v-if="currentRoom?.id !== 1" class="sidebar-tool danger" @click="showDeleteRoom = true; showSidebar = false"><span>🗑</span><span>删除本房间…</span></button>
        <button class="sidebar-tool" @click="cycleTheme"><span>{{ themeIcon }}</span><span>{{ themeLabel }}</span></button>
        <button class="sidebar-version" @click="showVersion">v{{ appVersion }}</button>
      </div>
    </aside>

    <!-- 主区域 -->
    <div class="main">
      <header class="topbar">
        <div class="topbar-row">
          <button class="menu-toggle" title="房间列表" @click="showSidebar = true">☰</button>
          <div class="room-title">
            <span>{{ currentRoom?.name || '速传' }}</span>
            <span v-if="currentRoom?.hasPassword" class="lock-icon">🔒</span>
          </div>
          <span v-if="stats" class="stats-chip">{{ stats.count }} 条 · {{ formatSize(stats.fileBytes || 0) }}</span>
        </div>
        <div class="search-box">
          <svg class="search-icon" viewBox="0 0 20 20"><circle cx="9" cy="9" r="6.2" fill="none" stroke="currentColor" stroke-width="1.8"/><line x1="13.6" y1="13.6" x2="17.5" y2="17.5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/></svg>
          <input v-model="searchQuery" class="search-input" type="search" placeholder="搜索本房间消息…" />
          <button v-if="searchQuery" class="search-clear" @click="searchQuery = ''">✕</button>
        </div>
      </header>

      <!-- 主消息列表 -->
      <main v-if="!showTrash" ref="listEl" class="list" @scroll="onScroll">
        <!-- 置顶条 -->
        <div v-if="pinnedMsg" class="pinned-bar">
          <span class="pinned-icon">📌</span>
          <span class="pinned-text">{{ pinnedMsg.type === 'file' ? pinnedMsg.fileName : pinnedMsg.content }}</span>
          <button class="pinned-unpin" title="取消置顶" @click="togglePin(pinnedMsg)">✕</button>
        </div>
        <button v-if="hasMore && !isSearching" class="load-more" :disabled="loadingOlder" @click="loadMore">
          {{ loadingOlder ? '加载中…' : '加载更早的消息' }}
        </button>
        <template v-for="g in groups" :key="g.day">
          <div class="day-sep">{{ g.day }}</div>
          <MessageItem v-for="m in g.items" :key="m.id" :msg="m" :pinned="currentRoom?.pinnedMsgId === m.id" @delete="deleteMessage" @preview="viewerMsg = $event" @pin="togglePin" />
        </template>
        <div v-for="u in uploads" :key="u.key" class="msg upload-item">
          <div class="bubble">
            <div class="upload-name">{{ u.name }} · {{ formatSize(u.size) }}</div>
            <div v-if="!u.error" class="upload-bar"><div class="upload-fill" :style="{ width: Math.round(u.progress * 100) + '%' }"></div></div>
            <div v-else class="upload-error">{{ u.error }}<button class="meta-btn" @click="dismissUpload(u.key)">知道了</button></div>
          </div>
        </div>
        <div v-if="isSearching && !searchResults.length" class="empty">没有匹配「{{ searchQuery.trim() }}」的消息</div>
        <div v-if="!isSearching && !messages.length && !uploads.length" class="empty">
          <div class="empty-icon">📨</div>
          <div class="empty-title">{{ currentRoom?.name || '速传' }}</div>
          <div class="empty-hint">输入文字按 Enter 发送<br />粘贴 / 拖入文件可直接上传</div>
        </div>
      </main>

      <!-- 回收站面板 -->
      <main v-else class="list trash-view">
        <div class="trash-header">
          <button class="back-btn" @click="showTrash = false">← 返回</button>
          <span class="trash-title">回收站</span>
          <button v-if="trashItems.length" class="meta-btn danger" @click="emptyTrash">清空</button>
        </div>
        <div v-if="!trashItems.length" class="empty"><div class="empty-icon">🗑</div><div class="empty-title">回收站是空的</div></div>
        <div v-for="m in trashItems" :key="m.id" class="trash-item">
          <div class="trash-content">
            <span v-if="m.type === 'file'" class="file-tag">{{ m.fileName }}</span>
            <span v-else>{{ m.content.slice(0, 60) }}{{ m.content.length > 60 ? '…' : '' }}</span>
          </div>
          <div class="trash-actions">
            <button class="meta-btn" @click="restoreMessage(m)">恢复</button>
            <button class="meta-btn danger" @click="permanentDelete(m)">彻底删除</button>
          </div>
        </div>
        <div class="trash-hint">删除的消息在回收站保留 3 天后自动清除</div>
      </main>

      <!-- 输入区 -->
      <footer v-if="!showTrash" class="composer">
        <div class="composer-box">
          <button class="attach-btn" title="选择文件" @click="fileInputEl.click()">
            <svg viewBox="0 0 20 20"><path d="M15.4 8.6l-6.2 6.2a3.2 3.2 0 01-4.5-4.5l7-7a2.2 2.2 0 013.1 3.1l-7 7" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/></svg>
          </button>
          <textarea ref="inputEl" v-model="draft" rows="1" placeholder="输入文字，Enter 发送" enterkeyhint="send" @input="autoGrow" @keydown.enter.exact.prevent="onEnterKey"></textarea>
          <button class="send-btn" :disabled="!draft.trim()" @click="sendText">发送</button>
          <input ref="fileInputEl" type="file" multiple hidden @change="onFilePick" />
        </div>
      </footer>
    </div>

    <div v-if="dragging" class="drop-overlay">松开鼠标上传文件</div>
    <ImageViewer v-if="viewerMsg" :msg="viewerMsg" @close="viewerMsg = null" />

    <!-- 新建房间 -->
    <div v-if="showRoomDialog" class="modal-backdrop" @click.self="showRoomDialog = false">
      <div class="modal">
        <h3>新建房间</h3>
        <input v-model="newRoomName" class="modal-input" placeholder="房间名称" maxlength="30" />
        <input v-model="newRoomPassword" class="modal-input" type="password" placeholder="密码（可选，留空则不加密）" />
        <div class="modal-actions">
          <button class="btn" @click="showRoomDialog = false">取消</button>
          <button class="btn primary" @click="createRoom">创建</button>
        </div>
      </div>
    </div>

    <!-- 房间密码登录 -->
    <div v-if="roomLoginForm" class="modal-backdrop" @click.self="roomLoginForm = null">
      <div class="modal">
        <h3>进入「{{ roomLoginForm.name }}」</h3>
        <input v-model="roomPassword" class="modal-input" type="password" placeholder="房间密码" @keydown.enter="submitRoomLogin" />
        <p v-if="roomLoginForm.error" class="modal-err">{{ roomLoginForm.error }}</p>
        <div class="modal-actions">
          <button class="btn" @click="roomLoginForm = null">取消</button>
          <button class="btn primary" @click="submitRoomLogin">进入</button>
        </div>
      </div>
    </div>

    <!-- 设置/修改房间密码 -->
    <div v-if="showPasswordDialog" class="modal-backdrop" @click.self="showPasswordDialog = false">
      <div class="modal">
        <h3>{{ currentRoom?.hasPassword ? '修改房间密码' : '设置房间密码' }}</h3>
        <p class="modal-text">房间「{{ currentRoom?.name }}」{{ currentRoom?.hasPassword ? '当前已加密。输入新密码替换旧密码；留空则取消加密。' : '当前未加密。输入密码即开启加密。' }}</p>
        <input v-model="passwordInput" class="modal-input" type="password" :placeholder="currentRoom?.hasPassword ? '新密码（留空取消加密）' : '新密码'" @keydown.enter="submitSetPassword" />
        <p v-if="passwordError" class="modal-err">{{ passwordError }}</p>
        <div class="modal-actions">
          <button class="btn" @click="showPasswordDialog = false; passwordInput = ''">取消</button>
          <button class="btn primary" @click="submitSetPassword">{{ currentRoom?.hasPassword ? (passwordInput.trim() ? '修改' : '取消加密') : '设置' }}</button>
        </div>
      </div>
    </div>

    <!-- 删除房间 -->
    <div v-if="showDeleteRoom" class="modal-backdrop" @click.self="showDeleteRoom = false">
      <div class="modal">
        <h3>删除房间「{{ currentRoom?.name }}」</h3>
        <p class="modal-text danger-text">将永久删除本房间及全部消息和文件，<strong>不进回收站，不可恢复</strong>。请输入管理密码确认。</p>
        <input v-model="adminPassword" class="modal-input" type="password" placeholder="管理密码（ADMIN_PASSWORD）" @keydown.enter="submitDeleteRoom" />
        <p v-if="deleteRoomError" class="modal-err">{{ deleteRoomError }}</p>
        <div class="modal-actions">
          <button class="btn" @click="showDeleteRoom = false; adminPassword = ''; deleteRoomError = ''">取消</button>
          <button class="btn danger" :disabled="deleting || !adminPassword" @click="submitDeleteRoom">{{ deleting ? '删除中…' : '永久删除' }}</button>
        </div>
      </div>
    </div>

    <!-- 清理旧消息 -->
    <div v-if="showCleanup" class="modal-backdrop" @click.self="showCleanup = false">
      <div class="modal">
        <h3>清理旧消息</h3>
        <p class="modal-text">将永久删除所选时间之前的全部消息（进入回收站）。</p>
        <div class="modal-actions">
          <button class="btn" @click="showCleanup = false">取消</button>
          <button class="btn danger" @click="doCleanup(90)">90 天前</button>
          <button class="btn danger" @click="doCleanup(30)">30 天前</button>
          <button class="btn danger" @click="doCleanup(7)">7 天前</button>
        </div>
      </div>
    </div>

    <!-- 清空 -->
    <div v-if="showClear" class="modal-backdrop" @click.self="showClear = false">
      <div class="modal">
        <h3>清空本房间</h3>
        <p class="modal-text">将删除「{{ currentRoom?.name }}」的全部消息（进入回收站）。</p>
        <div class="modal-actions">
          <button class="btn" @click="showClear = false">取消</button>
          <button class="btn danger" @click="doClear">确认清空</button>
        </div>
      </div>
    </div>

    <div v-if="toast" class="toast">{{ toast }}</div>
  </div>
</template>
