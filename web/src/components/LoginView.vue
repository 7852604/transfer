<script setup>
import { nextTick, onMounted, ref } from 'vue'
import { api } from '../api'

const emit = defineEmits(['done'])
const password = ref('')
const error = ref('')
const loading = ref(false)
const inputEl = ref(null)

onMounted(() => nextTick(() => inputEl.value?.focus()))

async function submit() {
  if (!password.value || loading.value) return
  loading.value = true
  error.value = ''
  try {
    await api.login(password.value)
    emit('done')
  } catch (e) {
    error.value = e.status === 429 ? '尝试太频繁，请一分钟后再试' : e.message || '登录失败'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-wrap">
    <form class="login-card" @submit.prevent="submit">
      <div class="login-logo">📨</div>
      <h1 class="login-title">速传</h1>
      <p class="login-sub">你的私人跨设备中转站</p>
      <input
        ref="inputEl"
        v-model="password"
        type="password"
        placeholder="访问密码"
        autocomplete="current-password"
        :disabled="loading"
      />
      <button type="submit" class="login-btn" :disabled="!password || loading">
        {{ loading ? '验证中…' : '进入' }}
      </button>
      <p v-if="error" class="login-err">{{ error }}</p>
    </form>
  </div>
</template>
