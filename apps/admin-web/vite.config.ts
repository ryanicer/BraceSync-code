import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5174,
    // 真实接口联调（T275）：VITE_DEV_API_TARGET=http://后端 时把 /api 转发过去；不设则行为不变（mock）
    proxy: process.env.VITE_DEV_API_TARGET
      ? { '/api': { target: process.env.VITE_DEV_API_TARGET, changeOrigin: true } }
      : undefined,
  },
})
