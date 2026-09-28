import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

const unitsTarget = process.env.UNITS_URL || 'http://localhost:8080'
const proxy = {
  '/api': { target: unitsTarget, changeOrigin: true },
  '/healthz': { target: unitsTarget, changeOrigin: true }
}

// 开发与预览（docker compose 中的 desk 容器）都把 /api 代理到 units 服务。
export default defineConfig({
  plugins: [vue()],
  server: {
    host: '0.0.0.0',
    port: 5173,
    proxy
  },
  preview: {
    host: '0.0.0.0',
    port: 5173,
    proxy
  }
})
