import { createApp } from 'vue'
import App from './App.vue'
import './style.css'

const app = createApp(App)
app.config.errorHandler = (err, _instance, info) => {
  console.error('[Vue]', info, err)
  window.__lastError = `${info}: ${err?.stack || err}`
}
app.mount('#app')
