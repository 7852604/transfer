import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 构建产物直接输出到 Go embed 目录，编译时打进二进制
export default defineConfig({
  plugins: [vue()],
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
