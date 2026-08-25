<script setup>
import { onMounted, ref } from 'vue'
import { api } from './api'
import TimelineView from './components/TimelineView.vue'

const ready = ref(false)

onMounted(async () => {
  // 跟随可视视口高度（键盘弹出时缩小）
  const vv = window.visualViewport
  const setAppHeight = () => {
    document.documentElement.style.setProperty('--app-height', `${vv?.height ?? window.innerHeight}px`)
  }
  vv?.addEventListener('resize', setAppHeight)
  setAppHeight()
  ready.value = true
})
</script>

<template>
  <div class="app">
    <TimelineView v-if="ready" />
    <div v-else class="boot">加载中…</div>
  </div>
</template>
