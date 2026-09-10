import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { readFileSync } from 'node:fs'

const pkg = JSON.parse(readFileSync('./package.json', 'utf-8'))
const buildTime = new Date().toISOString().slice(0, 16).replace('T', ' ')

// 构建产物直接输出到 Go embed 目录，编译时打进二进制
export default defineConfig({
  plugins: [vue()],
  define: {
    __APP_VERSION__: JSON.stringify(pkg.version),
    __BUILD_TIME__: JSON.stringify(buildTime),
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8787',
    },
  },
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
  },
})
