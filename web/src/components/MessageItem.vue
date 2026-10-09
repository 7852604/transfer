<script setup>
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { api } from '../api'
import { formatSize, formatTime, linkify } from '../utils'

const props = defineProps({
  msg: { type: Object, required: true },
  pinned: { type: Boolean, default: false },
  highlight: { type: String, default: '' },
})
const emit = defineEmits(['delete', 'preview', 'pin'])

const expanded = ref(false)
const collapsible = ref(false)
const copied = ref(false)
const confirming = ref(false)
const textEl = ref(null)
let confirmTimer = null
let copyTimer = null

const html = computed(() => linkify(props.msg.content, props.highlight))

const isVideo = computed(() => (props.msg.fileMime || '').startsWith('video/'))
const isAudio = computed(() => (props.msg.fileMime || '').startsWith('audio/'))

// 按 mime / 扩展名给文件配图标，少一次「下载再看」的猜测
const fileIcon = computed(() => {
  const name = (props.msg.fileName || '').toLowerCase()
  const mime = props.msg.fileMime || ''
  if (mime.startsWith('video/')) return '🎬'
  if (mime.startsWith('audio/')) return '🎵'
  if (mime === 'application/pdf' || name.endsWith('.pdf')) return '📕'
  if (/\.(zip|rar|7z|tar|gz|bz2|xz)$/.test(name)) return '🗜️'
  if (/\.(doc|docx|wps)$/.test(name)) return '📘'
  if (/\.(xls|xlsx|csv|et)$/.test(name)) return '📗'
  if (/\.(ppt|pptx|dps)$/.test(name)) return '📙'
  if (/\.(md|txt|rtf|log)$/.test(name)) return '📝'
  if (/\.(js|ts|jsx|tsx|go|py|java|c|cpp|h|rs|rb|php|sh|bash|zsh|sql|json|ya?ml|toml|xml|html|css|scss|vue|swift|kt)$/.test(name)) return '📜'
  return '📄'
})

onMounted(checkCollapse)
watch(
  () => props.msg.id,
  () => {
    expanded.value = false
    nextTick(checkCollapse)
  }
)

// 只在折叠态测量；展开态下元素不溢出，重测会把 collapsible 误置为 false
function checkCollapse() {
  const el = textEl.value
  if (!el || expanded.value) return
  collapsible.value = el.scrollHeight > el.clientHeight + 2
}

function toggleExpand() {
  expanded.value = !expanded.value
  nextTick(checkCollapse)
}

async function copy() {
  try {
    await navigator.clipboard.writeText(props.msg.content)
  } catch {
    // 非 HTTPS 环境降级
    const ta = document.createElement('textarea')
    ta.value = props.msg.content
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    ta.remove()
  }
  copied.value = true
  clearTimeout(copyTimer)
  copyTimer = setTimeout(() => (copied.value = false), 1200)
}

// 两段式确认：第一次点变成「确认删除？」，2.5 秒内再点才真删
function onDelete() {
  if (!confirming.value) {
    confirming.value = true
    clearTimeout(confirmTimer)
    confirmTimer = setTimeout(() => (confirming.value = false), 2500)
    return
  }
  clearTimeout(confirmTimer)
  confirming.value = false
  emit('delete', props.msg)
}
</script>

<template>
  <div class="msg" :data-msg-id="msg.id">
    <!-- 文字消息 -->
    <div v-if="msg.type === 'text'" class="bubble">
      <div ref="textEl" class="msg-text" :class="{ expanded }" v-html="html"></div>
      <button v-if="collapsible" class="expand-btn" @click="toggleExpand">
        {{ expanded ? '收起' : '展开全文' }}
      </button>
    </div>

    <!-- 图片消息 -->
    <div v-else-if="msg.isImage" class="bubble bubble-media">
      <img
        class="msg-img"
        :src="api.fileUrl(msg.fileId)"
        :alt="msg.fileName"
        loading="lazy"
        @click="emit('preview', msg)"
      />
    </div>

    <!-- 视频消息：直接在线播放 -->
    <div v-else-if="isVideo" class="bubble bubble-media">
      <video class="msg-video" :src="api.fileUrl(msg.fileId)" controls preload="metadata"></video>
    </div>

    <!-- 音频消息：直接在线播放 -->
    <div v-else-if="isAudio" class="bubble bubble-media">
      <audio class="msg-audio" :src="api.fileUrl(msg.fileId)" controls preload="metadata"></audio>
    </div>

    <!-- 文件消息 -->
    <a v-else class="bubble file-card" :href="api.downloadUrl(msg.fileId)" :download="msg.fileName">
      <div class="file-icon">{{ fileIcon }}</div>
      <div class="file-meta">
        <div class="file-name">{{ msg.fileName }}</div>
        <div class="file-size">{{ formatSize(msg.fileSize) }}</div>
      </div>
      <div class="file-dl">下载</div>
    </a>

    <div class="msg-meta">
      <span class="msg-time">{{ formatTime(msg.createdAt) }}</span>
      <button v-if="msg.type === 'text'" class="meta-btn" @click="copy">
        {{ copied ? '已复制' : '复制' }}
      </button>
      <button class="meta-btn" @click="emit('pin', props.msg)">
        {{ pinned ? '取消置顶' : '置顶' }}
      </button>
      <button class="meta-btn" :class="{ danger: confirming }" @click="onDelete">
        {{ confirming ? '确认删除？' : '删除' }}
      </button>
    </div>
  </div>
</template>
