import { defineConfig, loadEnv } from 'vite'
import uni from '@dcloudio/vite-plugin-uni'

// 注意：@dcloudio/vite-plugin-uni@5020320260806002 会把 import.meta.env 整体替换为 {}，
// 导致 VITE_* 变量在 mp-weixin 产物中全部丢失。
// 此处用 loadEnv 读取环境变量，再通过 define 静态注入自定义占位符绕开该缺陷。
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  return {
    plugins: [uni()],
    define: {
      __API_BASE_URL__: JSON.stringify(env.VITE_API_BASE_URL || ''),
      __USE_MOCK__: JSON.stringify(env.VITE_USE_MOCK !== 'false'),
    },
  }
})
