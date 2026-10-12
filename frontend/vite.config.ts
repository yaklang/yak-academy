import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 开发代理：前端请求 /api 转发到后端 academy-server（默认 :8080）
export default defineConfig({
  plugins: [vue()],
  server: {
    host: '127.0.0.1', // Windows 下 vite 默认仅监听 IPv6 localhost，显式指定
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
})