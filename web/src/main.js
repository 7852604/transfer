import { createApp } from 'vue'
import App from './App.vue'
import './style.css'

const app = createApp(App)
app.config.errorHandler = (err, _instance, info) => {
  console.error('[Vue]', info, err)
  window.__lastError = `${info}: ${err?.stack || err}`
}
app.mount('#app')

// PWA：注册 Service Worker（仅 HTTPS / localhost 生效，HTTP 访问时静默跳过）
if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch(() => {})
  })
}
