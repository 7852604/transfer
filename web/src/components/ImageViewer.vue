<script setup>
import { onMounted, onUnmounted } from 'vue'
import { api } from '../api'
import { formatSize } from '../utils'

const props = defineProps({ msg: { type: Object, required: true } })
const emit = defineEmits(['close'])

function onKey(e) {
  if (e.key === 'Escape') emit('close')
}
onMounted(() => window.addEventListener('keydown', onKey))
onUnmounted(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <div class="viewer" @click.self="emit('close')">
    <img :src="api.fileUrl(msg.fileId)" :alt="msg.fileName" @click.stop />
    <div class="viewer-bar" @click.stop>
      <span class="viewer-name">{{ msg.fileName }} · {{ formatSize(msg.fileSize) }}</span>
      <span class="viewer-actions">
        <a class="btn primary" :href="api.downloadUrl(msg.fileId)" :download="msg.fileName">下载</a>
        <button class="btn" @click="emit('close')">关闭</button>
      </span>
    </div>
  </div>
</template>
