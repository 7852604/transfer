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
const showMenu = ref(false)
const showRoomDialog = ref(false)
const showTrash = ref(false)
const showCleanup = ref(false)
const showClear = ref(false)
const viewerMsg = ref(null)
const dragging = ref(false)

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
  showMenu.value = false
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
    messages.value = []
    await loadInitial()
  } catch (e) { showToast(e.message) }
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
  showMenu.value = false
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
  showMenu.value = false
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
  <div class="timeline" @dragenter="onDragEnter" @dragover.prevent @dragleave="onDragLeave" @drop.prevent="onDrop">
    <header class="topbar">
      <div class="topbar-row">
        <div class="brand" @click="showMenu = !showMenu">
          <span class="brand-logo">📨</span>
          <span class="brand-name">{{ currentRoom?.name || '速传' }}</span>
          <span v-if="currentRoom?.hasPassword" class="lock-icon">🔒</span>
          <span class="chevron">▾</span>
        </div>
        <span v-if="stats" class="stats-chip">{{ stats.count }} 条 · {{ formatSize(stats.fileBytes || 0) }}</span>
      </div>
      <div class="search-box">
        <svg class="search-icon" viewBox="0 0 20 20"><circle cx="9" cy="9" r="6.2" fill="none" stroke="currentColor" stroke-width="1.8"/><line x1="13.6" y1="13.6" x2="17.5" y2="17.5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/></svg>
        <input v-model="searchQuery" class="search-input" type="search" placeholder="搜索本房间消息…" />
        <button v-if="searchQuery" class="search-clear" @click="searchQuery = ''">✕</button>
      </div>
    </header>

    <!-- 房间下拉菜单 -->
    <div v-if="showMenu" class="menu-backdrop" @click="showMenu = false"></div>
    <div v-if="showMenu" class="menu" @click.stop>
      <div class="menu-section-title">切换房间</div>
      <button v-for="r in rooms" :key="r.id" class="menu-item room-item" :class="{active: r.id === currentRoom?.id}" @click="switchRoom(r)">
        <span>{{ r.name }}</span>
        <span v-if="r.hasPassword" class="lock-icon">🔒</span>
        <span v-if="r.id === currentRoom?.id" class="check">✓</span>
      </button>
      <div class="menu-divider"></div>
      <button class="menu-item" @click="showRoomDialog = true; showMenu = false">＋ 新建房间…</button>
      <button class="menu-item" @click="openTrash">🗑 回收站</button>
      <button class="menu-item" @click="backupNow; showMenu = false">💾 立即备份</button>
      <button class="menu-item" @click="showCleanup = true; showMenu = false">🧹 清理旧消息…</button>
      <button class="menu-item danger" @click="showClear = true; showMenu = false">⚠ 清空本房间…</button>
    </div>

    <!-- 主消息列表 -->
    <main v-if="!showTrash" ref="listEl" class="list" @scroll="onScroll">
      <button v-if="hasMore && !isSearching" class="load-more" :disabled="loadingOlder" @click="loadMore">
        {{ loadingOlder ? '加载中…' : '加载更早的消息' }}
      </button>
      <template v-for="g in groups" :key="g.day">
        <div class="day-sep">{{ g.day }}</div>
        <MessageItem v-for="m in g.items" :key="m.id" :msg="m" @delete="deleteMessage" @preview="viewerMsg = $event" />
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
