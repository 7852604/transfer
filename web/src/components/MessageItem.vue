<script setup>
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { api } from '../api'
import { formatSize, formatTime, linkify } from '../utils'

const props = defineProps({ msg: { type: Object, required: true } })
const emit = defineEmits(['delete', 'preview'])

const expanded = ref(false)
const collapsible = ref(false)
const copied = ref(false)
const confirming = ref(false)
const textEl = ref(null)
let confirmTimer = null
let copyTimer = null

const html = computed(() => linkify(props.msg.content))

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
  <div class="msg">
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

    <!-- 文件消息 -->
    <a v-else class="bubble file-card" :href="api.downloadUrl(msg.fileId)" :download="msg.fileName">
      <div class="file-icon">📄</div>
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
      <button class="meta-btn" :class="{ danger: confirming }" @click="onDelete">
        {{ confirming ? '确认删除？' : '删除' }}
      </button>
    </div>
  </div>
</template>
