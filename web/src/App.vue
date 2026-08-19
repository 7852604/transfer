<script setup>
import { onMounted, ref } from 'vue'
import { api } from './api'
import LoginView from './components/LoginView.vue'
import TimelineView from './components/TimelineView.vue'

const view = ref('loading')

onMounted(async () => {
  // 跟随可视视口高度（键盘弹出时缩小），保证输入框始终可见
  const setAppHeight = () => {
    document.documentElement.style.setProperty(
      '--app-height',
      `${window.visualViewport?.height ?? window.innerHeight}px`
    )
  }
  window.visualViewport?.addEventListener('resize', setAppHeight)
  setAppHeight()

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
