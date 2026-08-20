<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api } from '../api'
import { formatDay, formatSize } from '../utils'
import MessageItem from './MessageItem.vue'
import ImageViewer from './ImageViewer.vue'

const emit = defineEmits(['logout'])

const messages = ref([])
const hasMore = ref(false)
const loadingOlder = ref(false)
const searchQuery = ref('')
const searchResults = ref(null) // null 表示非搜索态
const stats = ref(null)
const draft = ref('')
const uploads = ref([]) // {key, name, size, progress, error}
const toast = ref('')
const showMenu = ref(false)
const showCleanup = ref(false)
const showClear = ref(false)
const viewerMsg = ref(null)
const dragging = ref(false)
const listEl = ref(null)
const inputEl = ref(null)
const fileInputEl = ref(null)

const isSearching = computed(() => searchResults.value !== null)
const displayList = computed(() => (isSearching.value ? searchResults.value : messages.value))
const totalBytes = computed(() =>
  stats.value ? (stats.value.fileBytes || 0) + (stats.value.dbBytes || 0) : 0
)

const groups = computed(() => {
  const gs = []
  let lastDay = ''
  for (const m of displayList.value) {
    const day = formatDay(m.createdAt)
    if (day !== lastDay) {
      gs.push({ day, items: [m] })
      lastDay = day
    } else {
      gs[gs.length - 1].items.push(m)
    }
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

function handleAuthError(e) {
  if (e && e.status === 401) {
    emit('logout')
    return true
  }
  return false
}

async function loadInitial() {
  try {
    const data = await api.messages({ limit: 200 })
    messages.value = data.messages
    hasMore.value = data.hasMore
    scrollBottom()
    refreshStats()
  } catch (e) {
    if (!handleAuthError(e)) showToast('加载失败：' + e.message)
  }
}

async function poll() {
  if (document.hidden || isSearching.value) return
  const lastId = messages.value.length ? messages.value[messages.value.length - 1].id : 0
  try {
    const data = await api.messages({ after: lastId, limit: 100 })
    if (data.messages.length) {
      const nearBottom = isNearBottom()
      messages.value.push(...data.messages)
      if (nearBottom) scrollBottom()
      refreshStats()
    }
  } catch (e) {
    handleAuthError(e)
  }
}

function onVisible() {
  if (!document.hidden) {
    poll()
    refreshStats()
  }
}

// ---------- 滚动 ----------

function isNearBottom() {
  const el = listEl.value
  if (!el) return true
  return el.scrollHeight - el.scrollTop - el.clientHeight < 120
}

function scrollBottom() {
  nextTick(() => {
    const el = listEl.value
    if (el) el.scrollTop = el.scrollHeight
  })
}

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
    nextTick(() => {
      if (el) el.scrollTop = el.scrollHeight - prevHeight
    })
  } catch (e) {
    if (!handleAuthError(e)) showToast('加载更早消息失败')
  }
  loadingOlder.value = false
}

// ---------- 发送 ----------

function appendMessage(msg) {
  const last = messages.value[messages.value.length - 1]
  if (!last || msg.id > last.id) {
    messages.value.push(msg)
    scrollBottom()
  }
}

// 手机端聚焦输入框时，确保不被键盘遮挡
function onComposerFocus() {
  requestAnimationFrame(() => {
    inputEl.value?.scrollIntoView({ block: 'center', behavior: 'smooth' })
  })
}

async function sendText() {
  const content = draft.value.trim()
  if (!content) return
  draft.value = ''
  // 等 v-model 清空同步到 DOM 后再量高度，否则量到的还是旧内容的高度
  nextTick(() => autoGrow())
  try {
    const msg = await api.sendText(content)
    appendMessage(msg)
    refreshStats()
  } catch (e) {
    draft.value = content
    if (!handleAuthError(e)) showToast('发送失败：' + e.message)
  }
}

// 中文输入法选词、确认候选的 Enter 不触发发送
function onEnterKey(e) {
  if (e.isComposing || e.keyCode === 229) return
  sendText()
}

function autoGrow() {
  const el = inputEl.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = Math.min(el.scrollHeight, 160) + 'px'
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
      if (msgs[0]) {
        appendMessage(msgs[0])
        refreshStats()
      }
      uploads.value = uploads.value.filter((u) => u.key !== item.key)
    } catch (e) {
      if (handleAuthError(e)) return
      item.error = e.message || '上传失败'
    }
  }
}

function dismissUpload(key) {
  uploads.value = uploads.value.filter((u) => u.key !== key)
}

function onFilePick(e) {
  handleFiles(e.target.files)
  e.target.value = ''
}

function onPaste(e) {
  const files = e.clipboardData?.files
  if (files && files.length) {
    e.preventDefault()
    handleFiles(files)
  }
}

function onDragEnter(e) {
  e.preventDefault()
  dragDepth++
  dragging.value = true
}
function onDragLeave() {
  dragDepth--
  if (dragDepth <= 0) {
    dragging.value = false
    dragDepth = 0
  }
}
function onDrop(e) {
  dragging.value = false
  dragDepth = 0
  handleFiles(e.dataTransfer?.files)
}

// ---------- 搜索 ----------

watch(searchQuery, (q) => {
  clearTimeout(searchTimer)
  const query = q.trim()
  if (!query) {
    searchResults.value = null
    return
  }
  searchTimer = setTimeout(async () => {
    try {
      const data = await api.search(query)
      searchResults.value = data.messages
    } catch (e) {
      handleAuthError(e)
    }
  }, 300)
})

// ---------- 消息操作 ----------

async function deleteMessage(msg) {
  try {
    await api.deleteMessage(msg.id)
    messages.value = messages.value.filter((m) => m.id !== msg.id)
    if (isSearching.value) {
      searchResults.value = searchResults.value.filter((m) => m.id !== msg.id)
    }
    refreshStats()
  } catch (e) {
    if (!handleAuthError(e)) showToast('删除失败')
  }
}

// ---------- 菜单 / 清理 / 备份 ----------

async function backupNow() {
  showMenu.value = false
  showToast('备份中…')
  try {
    const r = await api.backupNow()
    showToast(`备份完成：${r.name}（${formatSize(r.size)}）`)
    refreshStats()
  } catch (e) {
    if (!handleAuthError(e)) showToast('备份失败：' + e.message)
  }
}

async function doCleanup(days) {
  showCleanup.value = false
  try {
    const r = await api.cleanup(days)
    showToast(`已删除 ${r.deleted} 条，释放 ${formatSize(r.freedBytes)}`)
    await loadInitial()
  } catch (e) {
    if (!handleAuthError(e)) showToast('清理失败')
  }
}

async function doClear() {
  showClear.value = false
  try {
    const r = await api.clearAll()
    showToast(`已清空 ${r.deleted} 条消息`)
    messages.value = []
    hasMore.value = false
    refreshStats()
  } catch (e) {
    if (!handleAuthError(e)) showToast('清空失败')
  }
}

async function logout() {
  try {
    await api.logout()
  } catch {
    /* ignore */
  }
  emit('logout')
}

async function refreshStats() {
  try {
    stats.value = await api.stats()
  } catch (e) {
    handleAuthError(e)
  }
}

function showToast(msg) {
  toast.value = msg
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => (toast.value = ''), 2600)
}
</script>

<template>
  <div
    class="timeline"
    @dragenter="onDragEnter"
    @dragover.prevent
    @dragleave="onDragLeave"
    @drop.prevent="onDrop"
  >
    <header class="topbar">
      <div class="topbar-row">
        <div class="brand">
          <span class="brand-logo">📨</span>
          <span class="brand-name">速传</span>
          <span v-if="stats" class="stats-chip">
            {{ stats.count }} 条 · {{ formatSize(totalBytes) }}
          </span>
        </div>
        <div class="topbar-actions">
          <button class="icon-btn" title="菜单" @click.stop="showMenu = !showMenu">⋯</button>
        </div>
      </div>
      <div class="search-box">
        <svg class="search-icon" viewBox="0 0 20 20" aria-hidden="true">
          <circle cx="9" cy="9" r="6.2" fill="none" stroke="currentColor" stroke-width="1.8" />
          <line x1="13.6" y1="13.6" x2="17.5" y2="17.5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
        </svg>
        <input v-model="searchQuery" class="search-input" type="search" placeholder="搜索历史消息…" />
        <button v-if="searchQuery" class="search-clear" type="button" title="清空" @click="searchQuery = ''">✕</button>
      </div>
      <div v-if="showMenu" class="menu-backdrop" @click="showMenu = false"></div>
      <div v-if="showMenu" class="menu" @click.stop>
        <button class="menu-item" @click="backupNow">立即备份</button>
        <button class="menu-item" @click="showCleanup = true; showMenu = false">清理旧消息…</button>
        <button class="menu-item danger" @click="showClear = true; showMenu = false">清空全部…</button>
        <button class="menu-item" @click="logout">退出登录</button>
      </div>
    </header>

    <main ref="listEl" class="list" @scroll="onScroll">
      <button v-if="hasMore && !isSearching" class="load-more" :disabled="loadingOlder" @click="loadMore">
        {{ loadingOlder ? '加载中…' : '加载更早的消息' }}
      </button>

      <template v-for="g in groups" :key="g.day">
        <div class="day-sep">{{ g.day }}</div>
        <MessageItem
          v-for="m in g.items"
          :key="m.id"
          :msg="m"
          @delete="deleteMessage"
          @preview="viewerMsg = $event"
        />
      </template>

      <div v-for="u in uploads" :key="u.key" class="msg upload-item">
        <div class="bubble">
          <div class="upload-name">{{ u.name }} · {{ formatSize(u.size) }}</div>
          <div v-if="!u.error" class="upload-bar"><div class="upload-fill" :style="{ width: Math.round(u.progress * 100) + '%' }"></div></div>
          <div v-else class="upload-error">
            {{ u.error }}
            <button class="meta-btn" @click="dismissUpload(u.key)">知道了</button>
          </div>
        </div>
      </div>

      <div v-if="isSearching && !searchResults.length" class="empty">没有匹配「{{ searchQuery.trim() }}」的消息</div>
      <div v-if="!isSearching && !messages.length && !uploads.length" class="empty">
        <div class="empty-icon">📨</div>
        <div class="empty-title">还没有消息</div>
        <div class="empty-hint">输入文字按 Enter 发送<br />粘贴 / 拖入文件可直接上传</div>
      </div>
    </main>

    <footer class="composer">
      <div class="composer-box">
        <button class="attach-btn" title="选择文件" @click="fileInputEl.click()">
          <svg viewBox="0 0 20 20" aria-hidden="true">
            <path
              d="M15.4 8.6l-6.2 6.2a3.2 3.2 0 01-4.5-4.5l7-7a2.2 2.2 0 013.1 3.1l-7 7"
              fill="none"
              stroke="currentColor"
              stroke-width="1.7"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </button>
        <textarea
          ref="inputEl"
          v-model="draft"
          rows="1"
          placeholder="输入文字，Enter 发送"
          enterkeyhint="send"
          @input="autoGrow"
          @focus="onComposerFocus"
          @keydown.enter.exact.prevent="onEnterKey"
        ></textarea>
        <button class="send-btn" :disabled="!draft.trim()" @click="sendText">发送</button>
      </div>
      <input ref="fileInputEl" type="file" multiple hidden @change="onFilePick" />
    </footer>

    <div v-if="dragging" class="drop-overlay">松开鼠标上传文件</div>

    <ImageViewer v-if="viewerMsg" :msg="viewerMsg" @close="viewerMsg = null" />

    <div v-if="showCleanup" class="modal-backdrop" @click.self="showCleanup = false">
      <div class="modal">
        <h3>清理旧消息</h3>
        <p class="modal-text">将永久删除所选时间之前的全部消息（含文件），删除后不可恢复。</p>
        <div class="modal-actions">
          <button class="btn" @click="showCleanup = false">取消</button>
          <button class="btn danger" @click="doCleanup(90)">90 天前</button>
          <button class="btn danger" @click="doCleanup(30)">30 天前</button>
          <button class="btn danger" @click="doCleanup(7)">7 天前</button>
        </div>
      </div>
    </div>

    <div v-if="showClear" class="modal-backdrop" @click.self="showClear = false">
      <div class="modal">
        <h3>清空全部</h3>
        <p class="modal-text">
          将删除全部 {{ stats?.count ?? 0 }} 条消息和文件，并释放 {{ formatSize(totalBytes) }} 空间。此操作不可恢复。
        </p>
        <div class="modal-actions">
          <button class="btn" @click="showClear = false">取消</button>
          <button class="btn danger" @click="doClear">确认清空</button>
        </div>
      </div>
    </div>

    <div v-if="toast" class="toast">{{ toast }}</div>
  </div>
</template>
