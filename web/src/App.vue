<script setup>
import { onMounted, ref } from 'vue'
import { api } from './api'
import LoginView from './components/LoginView.vue'
import TimelineView from './components/TimelineView.vue'

const view = ref('loading')

onMounted(async () => {
  // 跟随可视视口高度（键盘弹出时缩小），保证输入框始终可见
  const vv = window.visualViewport
  const setAppHeight = () => {
    document.documentElement.style.setProperty('--app-height', `${vv?.height ?? window.innerHeight}px`)
  }
  vv?.addEventListener('resize', setAppHeight)
  vv?.addEventListener('scroll', setAppHeight)
  setAppHeight()

  // iOS Safari 键盘弹出时 visualViewport 会偏移，需要把聚焦的输入框拉回可视区
  if (vv) {
    vv.addEventListener('resize', () => {
      const el = document.activeElement
      if (el && (el.tagName === 'TEXTAREA' || el.tagName === 'INPUT')) {
        // 等浏览器布局稳定后再滚
        requestAnimationFrame(() => el.scrollIntoView({ block: 'center', behavior: 'smooth' }))
      }
    })
  }

  try {
    await api.stats()
    view.value = 'ready'
  } catch (e) {
    view.value = e.status === 401 ? 'login' : 'error'
  }
})
</script>

<template>
  <div class="app">
    <div v-if="view === 'loading'" class="boot">加载中…</div>
    <LoginView v-else-if="view === 'login'" @done="view = 'ready'" />
    <TimelineView v-else-if="view === 'ready'" @logout="view = 'login'" />
    <div v-else class="boot">
      无法连接服务器
      <button class="btn-retry" @click="location.reload()">重试</button>
    </div>
  </div>
</template>
