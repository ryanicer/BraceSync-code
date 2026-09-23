import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { ADMIN_BASE } from './src/utils/mount'

export default defineConfig({
  // T336：dev 与 build 同用 /admin/ —— 挂载点由 nginx 决定（scripts/deploy/nginx.conf 的 location /admin/），
  // 本地开发地址随之变为 http://localhost:5174/admin/，深链/刷新行为与 staging 一致，不再是「本地好、线上弹首页」。
  base: ADMIN_BASE,
  plugins: [vue()],
  server: {
    port: 5174,
    // 真实接口联调（T275）：VITE_DEV_API_TARGET=http://后端 时把 /api 转发过去；不设则行为不变（mock）
    proxy: process.env.VITE_DEV_API_TARGET
      ? { '/api': { target: process.env.VITE_DEV_API_TARGET, changeOrigin: true } }
      : undefined,
  },
})
